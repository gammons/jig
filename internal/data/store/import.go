package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gammons/jig/internal/core"
)

// ImportSession inserts sess, msgs, and todos in one transaction, for
// bringing a session in from an external source (e.g. opencode). It fails
// if sess.ID already exists, or if any message references a session that
// does not exist, rolling back the whole import.
func (s *Store) ImportSession(ctx context.Context, sess core.Session, msgs []core.Message, todos []core.Todo) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (id, parent_id, title, agent, model, effort, cwd, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			string(sess.ID), string(sess.ParentID), sess.Title, sess.Agent, sess.Model, string(sess.Effort), sess.Cwd,
			sess.CreatedAt.UnixMilli(), sess.UpdatedAt.UnixMilli(),
		); err != nil {
			return fmt.Errorf("store: importing session %s: %w", sess.ID, err)
		}

		for _, m := range msgs {
			if err := upsertMessage(ctx, tx, m); err != nil {
				return err
			}
			for seq, p := range m.Parts {
				if err := insertPart(ctx, tx, m.ID, seq, p); err != nil {
					return err
				}
			}
		}

		for seq, t := range todos {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO todos (session_id, seq, content, status) VALUES (?, ?, ?, ?)
			`, string(sess.ID), seq, t.Content, t.Status); err != nil {
				return fmt.Errorf("store: importing todo %d for session %s: %w", seq, sess.ID, err)
			}
		}

		return nil
	})
}

// SessionExists reports whether a session with the given id exists.
func (s *Store) SessionExists(ctx context.Context, id core.SessionID) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, string(id)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: checking session %s exists: %w", id, err)
	}
	return true, nil
}
