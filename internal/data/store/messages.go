package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gammons/jig/internal/core"
)

// SaveMessage upserts the message row and replaces all of its parts, in one
// transaction. Saving a message whose SessionID does not exist fails with a
// foreign key violation rather than silently inserting.
func (s *Store) SaveMessage(ctx context.Context, m core.Message) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := upsertMessage(ctx, tx, m); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM parts WHERE message_id = ?`, string(m.ID)); err != nil {
			return fmt.Errorf("store: clearing parts for message %s: %w", m.ID, err)
		}
		for seq, p := range m.Parts {
			if err := insertPart(ctx, tx, m.ID, seq, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertMessage(ctx context.Context, tx *sql.Tx, m core.Message) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO messages (
			id, session_id, role, agent, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			cost_usd, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			session_id = excluded.session_id,
			role = excluded.role,
			agent = excluded.agent,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cache_write_tokens = excluded.cache_write_tokens,
			cost_usd = excluded.cost_usd,
			status = excluded.status,
			created_at = excluded.created_at
	`,
		string(m.ID), string(m.SessionID), string(m.Role), m.Agent, m.Model,
		m.Usage.Input, m.Usage.Output, m.Usage.CacheRead, m.Usage.CacheWrite,
		m.CostUSD, string(m.Status), m.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("store: saving message %s: %w", m.ID, err)
	}
	return nil
}

// textData is the data_json shape for text, reasoning, and compaction
// parts.
type textData struct {
	Text string `json:"text"`
}

func insertPart(ctx context.Context, tx *sql.Tx, messageID core.MessageID, seq int, p core.Part) error {
	data, err := encodePart(p)
	if err != nil {
		return fmt.Errorf("store: encoding part %d of message %s: %w", seq, messageID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO parts (message_id, seq, kind, data_json) VALUES (?, ?, ?, ?)
	`, string(messageID), seq, string(p.Kind), data); err != nil {
		return fmt.Errorf("store: inserting part %d of message %s: %w", seq, messageID, err)
	}
	return nil
}

func encodePart(p core.Part) ([]byte, error) {
	switch p.Kind {
	case core.PartText, core.PartReasoning, core.PartCompaction:
		return json.Marshal(textData{Text: p.Text})
	case core.PartToolCall:
		return json.Marshal(p.Call)
	case core.PartToolResult:
		return json.Marshal(p.Result)
	case core.PartAttachment:
		return json.Marshal(p.Attachment)
	default:
		return nil, fmt.Errorf("unknown part kind %q", p.Kind)
	}
}

func decodePart(kind, data string) (core.Part, error) {
	switch core.PartKind(kind) {
	case core.PartText, core.PartReasoning, core.PartCompaction:
		var td textData
		if err := json.Unmarshal([]byte(data), &td); err != nil {
			return core.Part{}, err
		}
		return core.Part{Kind: core.PartKind(kind), Text: td.Text}, nil
	case core.PartToolCall:
		var call core.ToolCall
		if err := json.Unmarshal([]byte(data), &call); err != nil {
			return core.Part{}, err
		}
		// json.RawMessage's UnmarshalJSON copies the literal "null" bytes
		// verbatim instead of decoding them to nil, so a nil Input that was
		// marshaled to JSON null comes back as a non-nil 4-byte
		// json.RawMessage("null") rather than nil. Normalize that one case
		// back to nil so nil Input round-trips exactly; any other Input,
		// including an explicit "{}", is left untouched.
		if string(call.Input) == "null" {
			call.Input = nil
		}
		return core.Part{Kind: core.PartToolCall, Call: &call}, nil
	case core.PartToolResult:
		var result core.ToolResult
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			return core.Part{}, err
		}
		return core.Part{Kind: core.PartToolResult, Result: &result}, nil
	case core.PartAttachment:
		var att core.Attachment
		if err := json.Unmarshal([]byte(data), &att); err != nil {
			return core.Part{}, err
		}
		return core.Part{Kind: core.PartAttachment, Attachment: &att}, nil
	default:
		return core.Part{}, fmt.Errorf("unknown part kind %q", kind)
	}
}

// ListMessages returns id's messages ordered by created_at then id, each
// with its parts ordered by seq.
func (s *Store) ListMessages(ctx context.Context, id core.SessionID) ([]core.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, agent, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			cost_usd, status, created_at
		FROM messages WHERE session_id = ?
		ORDER BY created_at ASC, id ASC
	`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: listing messages for session %s: %w", id, err)
	}
	defer rows.Close()

	var messages []core.Message
	for rows.Next() {
		m, err := scanMessage(rows, id)
		if err != nil {
			return nil, fmt.Errorf("store: listing messages for session %s: %w", id, err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing messages for session %s: %w", id, err)
	}

	for i := range messages {
		parts, err := s.loadParts(ctx, messages[i].ID)
		if err != nil {
			return nil, err
		}
		messages[i].Parts = parts
	}
	return messages, nil
}

func scanMessage(rows *sql.Rows, sessionID core.SessionID) (core.Message, error) {
	var m core.Message
	var msgID, role, status string
	var created int64
	if err := rows.Scan(
		&msgID, &role, &m.Agent, &m.Model,
		&m.Usage.Input, &m.Usage.Output, &m.Usage.CacheRead, &m.Usage.CacheWrite,
		&m.CostUSD, &status, &created,
	); err != nil {
		return core.Message{}, err
	}
	m.ID = core.MessageID(msgID)
	m.SessionID = sessionID
	m.Role = core.Role(role)
	m.Status = core.MessageStatus(status)
	m.CreatedAt = time.UnixMilli(created)
	return m, nil
}

func (s *Store) loadParts(ctx context.Context, messageID core.MessageID) ([]core.Part, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, data_json FROM parts WHERE message_id = ? ORDER BY seq ASC
	`, string(messageID))
	if err != nil {
		return nil, fmt.Errorf("store: loading parts for message %s: %w", messageID, err)
	}
	defer rows.Close()

	var parts []core.Part
	for rows.Next() {
		var kind, data string
		if err := rows.Scan(&kind, &data); err != nil {
			return nil, fmt.Errorf("store: loading parts for message %s: %w", messageID, err)
		}
		p, err := decodePart(kind, data)
		if err != nil {
			return nil, fmt.Errorf("store: decoding part of message %s: %w", messageID, err)
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: loading parts for message %s: %w", messageID, err)
	}
	return parts, nil
}
