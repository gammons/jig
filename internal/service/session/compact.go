package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gammons/jig/internal/core"
)

// ErrNothingToCompact is returned by Compact when no message follows the
// last compaction.
var ErrNothingToCompact = errors.New("nothing to compact")

// compactionAgent is the hidden built-in agent whose prompt drives Compact.
const compactionAgent = "compaction"

// Compact summarizes the messages since the last compaction (including
// that compaction's summary) on the session's small model, and saves the
// summary as a new assistant message holding a single PartCompaction, so
// History starts from it from now on.
func (s *Service) Compact(ctx context.Context, id core.SessionID) error {
	sess, err := s.d.Store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	msgs, err := s.d.Store.ListMessages(ctx, id)
	if err != nil {
		return err
	}
	start := max(lastCompaction(msgs), 0)
	segment := msgs[start:]
	if !hasNewMessages(segment) {
		return ErrNothingToCompact
	}

	summary, model, err := s.complete(ctx, compactionAgent, sess, render(segment))
	if err != nil {
		return err
	}
	return s.d.Store.SaveMessage(ctx, core.Message{
		ID:        core.MessageID(s.d.IDs.Next("msg")),
		SessionID: id,
		Role:      core.RoleAssistant,
		Agent:     compactionAgent,
		Model:     model.String(),
		Parts:     []core.Part{{Kind: core.PartCompaction, Text: summary}},
		Status:    core.StatusComplete,
		CreatedAt: s.after(msgs),
	})
}

// hasNewMessages reports whether segment has any message that is not a
// compaction.
func hasNewMessages(segment []core.Message) bool {
	for _, m := range segment {
		if !isCompaction(m) {
			return true
		}
	}
	return false
}

// after returns a CreatedAt that sorts after every message in msgs, at the
// store's millisecond resolution: now, or 1ms past the last message.
func (s *Service) after(msgs []core.Message) time.Time {
	now := s.d.Clock.Now().Truncate(time.Millisecond)
	if n := len(msgs); n > 0 {
		if floor := msgs[n-1].CreatedAt.Add(time.Millisecond); now.Before(floor) {
			return floor
		}
	}
	return now
}

// render formats msgs as a plain-text transcript: "user: …" and
// "assistant: …" lines, tool calls as "[tool <name>]", and a prior
// compaction as "summary: …". Reasoning and tool results are omitted.
func render(msgs []core.Message) string {
	blocks := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if b := renderMessage(m); b != "" {
			blocks = append(blocks, b)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func renderMessage(m core.Message) string {
	var lines []string
	prefix := string(m.Role) + ": "
	for _, p := range m.Parts {
		switch p.Kind {
		case core.PartText:
			if strings.TrimSpace(p.Text) != "" {
				lines = append(lines, p.Text)
			}
		case core.PartToolCall:
			if p.Call != nil {
				lines = append(lines, "[tool "+p.Call.Name+"]")
			}
		case core.PartCompaction:
			prefix = "summary: "
			lines = append(lines, p.Text)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return prefix + strings.Join(lines, "\n")
}
