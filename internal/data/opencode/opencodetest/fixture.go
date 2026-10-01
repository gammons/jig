// Package opencodetest builds throwaway opencode SQLite databases for
// tests: internal/data/opencode's own reader tests, and e2e tests that
// exercise the built jig binary against a fixture DB. It is a non-test
// package (not _test.go) so e2e/ can import it.
package opencodetest

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// escapeDSNPath percent-encodes the characters modernc.org/sqlite's DSN
// parser treats specially (a literal '?' splits the dsn at the path's
// own query-like suffix, and '#' would similarly be read as a URI
// fragment), so a path containing one of them names exactly that file
// rather than a different, truncated one. This is a second copy of
// internal/data/opencode's escapeDSNPath: opencodetest can't import that
// unexported helper, and internal/data/opencode must not import
// opencodetest (layer rule), so each package keeps its own tiny copy
// rather than share one.
func escapeDSNPath(path string) string {
	r := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")
	return r.Replace(path)
}

// Session is one opencode session_v2 row to write, along with its
// messages and todos.
type Session struct {
	ID        string
	ParentID  string // "" writes NULL
	Directory string
	Title     string // "" writes NULL
	Agent     string // "" writes NULL
	Model     string // raw JSON for the model column; "" writes NULL
	Created   int64
	Updated   int64
	Messages  []Message
	Todos     []Todo
}

// Message is one opencode session_message row to write.
type Message struct {
	ID      string
	Type    string
	Data    string
	Seq     int
	Created int64
}

// Todo is one opencode todo row to write.
type Todo struct {
	Content  string
	Status   string
	Priority string
	Position int
}

// Write creates a new SQLite database at path holding opencode's
// session_v2, session_message, and todo tables (the columns the reader
// uses, plus project_id/slug/version NOT NULL dummy values on session_v2),
// and inserts sessions, their messages, and their todos.
func Write(path string, sessions []Session) error {
	db, err := sql.Open("sqlite", "file:"+escapeDSNPath(path))
	if err != nil {
		return fmt.Errorf("opencodetest: opening %s: %w", path, err)
	}
	defer db.Close()

	if err := createSchema(db); err != nil {
		return err
	}
	for _, s := range sessions {
		if err := writeSession(db, s); err != nil {
			return err
		}
	}
	return nil
}

func createSchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE session_v2 (
	id TEXT PRIMARY KEY,
	parent_id TEXT,
	project_id TEXT NOT NULL,
	directory TEXT NOT NULL,
	title TEXT,
	slug TEXT NOT NULL,
	version TEXT NOT NULL,
	agent TEXT,
	model TEXT,
	time_created INTEGER NOT NULL,
	time_updated INTEGER NOT NULL
);
CREATE TABLE session_message (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	type TEXT NOT NULL,
	seq INTEGER NOT NULL,
	time_created INTEGER NOT NULL,
	data TEXT NOT NULL
);
CREATE TABLE todo (
	session_id TEXT NOT NULL,
	content TEXT NOT NULL,
	status TEXT NOT NULL,
	priority TEXT,
	position INTEGER NOT NULL
);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("opencodetest: creating schema: %w", err)
	}
	return nil
}

func writeSession(db *sql.DB, s Session) error {
	parentID := nullableString(s.ParentID)
	title := nullableString(s.Title)
	agent := nullableString(s.Agent)
	model := nullableString(s.Model)

	_, err := db.Exec(
		`INSERT INTO session_v2 (id, parent_id, project_id, directory, title, slug, version, agent, model, time_created, time_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, parentID, "proj_fixture", s.Directory, title, "fixture-slug", "fixture-version", agent, model, s.Created, s.Updated,
	)
	if err != nil {
		return fmt.Errorf("opencodetest: inserting session %s: %w", s.ID, err)
	}

	for _, m := range s.Messages {
		if _, err := db.Exec(
			`INSERT INTO session_message (id, session_id, type, seq, time_created, data) VALUES (?, ?, ?, ?, ?, ?)`,
			m.ID, s.ID, m.Type, m.Seq, m.Created, m.Data,
		); err != nil {
			return fmt.Errorf("opencodetest: inserting message %s: %w", m.ID, err)
		}
	}

	for _, td := range s.Todos {
		if _, err := db.Exec(
			`INSERT INTO todo (session_id, content, status, priority, position) VALUES (?, ?, ?, ?, ?)`,
			s.ID, td.Content, td.Status, nullableString(td.Priority), td.Position,
		); err != nil {
			return fmt.Errorf("opencodetest: inserting todo for %s: %w", s.ID, err)
		}
	}
	return nil
}

// nullableString returns nil for "" so an empty value writes SQL NULL,
// else s itself.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
