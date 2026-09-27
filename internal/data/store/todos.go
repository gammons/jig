package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gammons/jig/internal/core"
)

// ReplaceTodos deletes id's existing todos and inserts todos in their given
// order, in one transaction. todos[i]'s position becomes its seq.
func (s *Store) ReplaceTodos(ctx context.Context, id core.SessionID, todos []core.Todo) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM todos WHERE session_id = ?`, string(id)); err != nil {
			return fmt.Errorf("store: clearing todos for session %s: %w", id, err)
		}
		for seq, t := range todos {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO todos (session_id, seq, content, status) VALUES (?, ?, ?, ?)
			`, string(id), seq, t.Content, t.Status); err != nil {
				return fmt.Errorf("store: inserting todo %d for session %s: %w", seq, id, err)
			}
		}
		return nil
	})
}

// ListTodos returns id's todos ordered by seq.
func (s *Store) ListTodos(ctx context.Context, id core.SessionID) ([]core.Todo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT content, status FROM todos WHERE session_id = ? ORDER BY seq ASC
	`, string(id))
	if err != nil {
		return nil, fmt.Errorf("store: listing todos for session %s: %w", id, err)
	}
	defer rows.Close()

	var todos []core.Todo
	for rows.Next() {
		var t core.Todo
		if err := rows.Scan(&t.Content, &t.Status); err != nil {
			return nil, fmt.Errorf("store: listing todos for session %s: %w", id, err)
		}
		todos = append(todos, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing todos for session %s: %w", id, err)
	}
	return todos, nil
}
