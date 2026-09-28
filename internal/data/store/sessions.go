package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gammons/jig/internal/core"
)

// CreateSession inserts a new session row.
func (s *Store) CreateSession(ctx context.Context, sess core.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, parent_id, title, agent, model, cwd, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		string(sess.ID), string(sess.ParentID), sess.Title, sess.Agent, sess.Model, sess.Cwd,
		sess.CreatedAt.UnixMilli(), sess.UpdatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("store: creating session %s: %w", sess.ID, err)
	}
	return nil
}

// UpdateSession updates title, agent, model, and updated_at for an existing
// session. It returns ErrNotFound if id does not exist.
func (s *Store) UpdateSession(ctx context.Context, sess core.Session) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET title = ?, agent = ?, model = ?, updated_at = ?
		WHERE id = ?
	`, sess.Title, sess.Agent, sess.Model, sess.UpdatedAt.UnixMilli(), string(sess.ID))
	if err != nil {
		return fmt.Errorf("store: updating session %s: %w", sess.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: updating session %s: %w", sess.ID, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsNotFound reports whether err is (or wraps) ErrNotFound.
func (s *Store) IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// GetSession returns the session with the given id, or ErrNotFound if none
// exists.
func (s *Store) GetSession(ctx context.Context, id core.SessionID) (core.Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, parent_id, title, agent, model, cwd, created_at, updated_at
		FROM sessions WHERE id = ?
	`, string(id))
	return scanSession(row)
}

// ListSessions returns sessions whose parent_id matches parent ("" for
// roots), ordered newest updated_at first (then id descending), limited to
// limit rows unless limit <= 0.
func (s *Store) ListSessions(ctx context.Context, parent core.SessionID, limit int) ([]core.Session, error) {
	query := `
		SELECT id, parent_id, title, agent, model, cwd, created_at, updated_at
		FROM sessions WHERE parent_id = ?
		ORDER BY updated_at DESC, id DESC
	`
	args := []any{string(parent)}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing sessions: %w", err)
	}
	defer rows.Close()

	var sessions []core.Session
	for rows.Next() {
		sess, err := scanSessionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: listing sessions: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing sessions: %w", err)
	}
	return sessions, nil
}

// ListRootsByCwd returns root sessions (parent_id = "") whose cwd matches
// cwd exactly, ordered newest updated_at first (then id descending),
// limited to limit rows unless limit <= 0. Callers canonicalize cwd (see
// pathid.Key) before calling.
func (s *Store) ListRootsByCwd(ctx context.Context, cwd string, limit int) ([]core.Session, error) {
	query := `
		SELECT id, parent_id, title, agent, model, cwd, created_at, updated_at
		FROM sessions WHERE parent_id = '' AND cwd = ?
		ORDER BY updated_at DESC, id DESC
	`
	args := []any{cwd}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing sessions for cwd %q: %w", cwd, err)
	}
	defer rows.Close()

	var sessions []core.Session
	for rows.Next() {
		sess, err := scanSessionRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: listing sessions for cwd %q: %w", cwd, err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing sessions for cwd %q: %w", cwd, err)
	}
	return sessions, nil
}

// sessionScanner is the subset of *sql.Row and *sql.Rows that scanSessionRow
// needs, so it can share scanning logic between GetSession's single row and
// ListSessions' rows.
type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (core.Session, error) {
	sess, err := scanSessionRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Session{}, ErrNotFound
	}
	if err != nil {
		return core.Session{}, fmt.Errorf("store: getting session: %w", err)
	}
	return sess, nil
}

func scanSessionRow(row sessionScanner) (core.Session, error) {
	var sess core.Session
	var id, parentID string
	var created, updated int64
	if err := row.Scan(&id, &parentID, &sess.Title, &sess.Agent, &sess.Model, &sess.Cwd, &created, &updated); err != nil {
		return core.Session{}, err
	}
	sess.ID = core.SessionID(id)
	sess.ParentID = core.SessionID(parentID)
	sess.CreatedAt = time.UnixMilli(created)
	sess.UpdatedAt = time.UnixMilli(updated)
	return sess, nil
}
