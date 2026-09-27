// Package store is jig's SQLite-backed persistence layer: sessions,
// messages, parts, and todos. It embeds its own migrations and applies them
// on Open, and every pooled connection gets the same WAL/foreign-keys/
// busy-timeout pragmas via the connection DSN.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned by GetSession and UpdateSession when no row
// matches the given id.
var ErrNotFound = errors.New("store: not found")

// Store is jig's SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens the SQLite database at path, creating its parent directory if
// needed, then applies any pending embedded migrations. Every pooled
// connection gets WAL journaling, foreign key enforcement, and a 5 second
// busy timeout via the connection DSN.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: creating data dir %s: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connecting to %s: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// withTx runs fn inside a transaction, committing on success and rolling
// back if fn returns an error.
func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: committing transaction: %w", err)
	}
	return nil
}

// migrate applies every embedded migration whose version is not already
// recorded in schema_migrations, in filename order, each in its own
// transaction alongside the row that records it.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("store: creating schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: listing migrations: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := applyIfPending(ctx, db, name); err != nil {
			return fmt.Errorf("store: applying migration %s: %w", name, err)
		}
	}
	return nil
}

func applyIfPending(ctx context.Context, db *sql.DB, name string) error {
	version, err := migrationVersion(name)
	if err != nil {
		return err
	}
	applied, err := isApplied(ctx, db, version)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	script, err := migrationsFS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("reading: %w", err)
	}
	return withTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
		return err
	})
}

func migrationVersion(name string) (int, error) {
	base := filepath.Base(name)
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("migration filename %q missing version prefix", base)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration filename %q has non-numeric version: %w", base, err)
	}
	return version, nil
}

func isApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking migration %d: %w", version, err)
	}
	return count > 0, nil
}
