package opencode

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/gammons/jig/internal/core"

	_ "modernc.org/sqlite"
)

// NotOpencodeError is returned by Open when path is a SQLite database
// that lacks the session_v2 or session_message tables opencode's current
// schema requires.
type NotOpencodeError struct {
	Path string
}

func (e *NotOpencodeError) Error() string {
	return fmt.Sprintf("%s: not an opencode database (no session_v2 table)", e.Path)
}

// Options configures Open. Agents is the set of agent names jig knows
// (built-ins plus configured), used to normalize a session's and its
// assistant messages' Agent (spec §5.1/§5.3).
type Options struct {
	Agents []string
}

// Item is one session as Each yields it: its translated core.Session,
// its core.Message slice in order, its core.Todo slice in position
// order, and the Stats accumulated while translating it.
type Item struct {
	Session  core.Session
	Messages []core.Message
	Todos    []core.Todo
	Stats    Stats
}

// Source is a read-only handle onto an opencode SQLite database.
type Source struct {
	db     *sql.DB
	agents map[string]bool
}

// Open opens the opencode database at path read-only and checks that it
// has the current session_v2/session_message schema. A missing path
// returns an error wrapping fs.ErrNotExist; a SQLite file lacking either
// table returns a *NotOpencodeError.
func Open(ctx context.Context, path string, opts Options) (*Source, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("opencode: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opencode: opening %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("opencode: connecting to %s: %w", path, err)
	}

	if err := checkSchema(ctx, db); err != nil {
		_ = db.Close()
		var ne *NotOpencodeError
		if errors.As(err, &ne) {
			ne.Path = path
			return nil, ne
		}
		return nil, err
	}

	agents := make(map[string]bool, len(opts.Agents))
	for _, a := range opts.Agents {
		agents[a] = true
	}

	return &Source{db: db, agents: agents}, nil
}

// checkSchema returns a *NotOpencodeError if session_v2 or
// session_message is missing from the database.
func checkSchema(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"session_v2", "session_message"} {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return &NotOpencodeError{}
		}
		if err != nil {
			return fmt.Errorf("opencode: checking schema: %w", err)
		}
	}
	return nil
}

// Close closes the underlying database connection.
func (s *Source) Close() error {
	return s.db.Close()
}

// sessionNode is one entry of the in-memory (id, parent_id, time_created)
// index Each builds before walking.
type sessionNode struct {
	id       string
	parentID string
	created  int64
}

// Each walks every session in the database in an order where every
// parent precedes its children: roots (no parent, or a parent naming a
// missing or cyclic session) ordered by (time_created, id), each
// followed by its children recursively in the same order. For each
// session it reads the session_v2 row, streams session_message rows in
// seq order into a translator, reads todo rows in position order, and
// calls fn with the resulting Item. A non-nil error from fn, or ctx
// cancellation, stops the walk.
func (s *Source) Each(ctx context.Context, fn func(Item) error) error {
	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return err
	}

	children := map[string][]sessionNode{}
	for _, n := range nodes {
		children[n.parentID] = append(children[n.parentID], n)
	}
	for parent := range children {
		sortNodes(children[parent])
	}

	byID := make(map[string]sessionNode, len(nodes))
	for _, n := range nodes {
		byID[n.id] = n
	}

	visited := map[string]bool{}

	var roots []sessionNode
	for _, n := range nodes {
		if n.parentID == "" {
			roots = append(roots, n)
			continue
		}
		if _, ok := byID[n.parentID]; !ok {
			roots = append(roots, n)
		}
	}
	sortNodes(roots)

	for _, root := range roots {
		if err := s.walk(ctx, root, children, visited, fn); err != nil {
			return err
		}
	}

	// Second pass: any node not yet visited is part of a cycle (every
	// member names a parent that exists, but no root reaches it). Yield
	// each unvisited node, in (created, id) order, as a root of its own
	// subtree.
	var remaining []sessionNode
	for _, n := range nodes {
		if !visited[n.id] {
			remaining = append(remaining, n)
		}
	}
	sortNodes(remaining)
	for _, n := range remaining {
		if visited[n.id] {
			continue
		}
		if err := s.walk(ctx, n, children, visited, fn); err != nil {
			return err
		}
	}

	return nil
}

// sortNodes sorts ns in place by (created, id).
func sortNodes(ns []sessionNode) {
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].created != ns[j].created {
			return ns[i].created < ns[j].created
		}
		return ns[i].id < ns[j].id
	})
}

// walk visits node and then its children (in children[node.id]'s order),
// depth-first, skipping any node already visited (breaking a cycle), and
// calling fn for each.
func (s *Source) walk(ctx context.Context, node sessionNode, children map[string][]sessionNode, visited map[string]bool, fn func(Item) error) error {
	if visited[node.id] {
		return nil
	}
	visited[node.id] = true

	if err := ctx.Err(); err != nil {
		return err
	}

	item, err := s.loadItem(ctx, node.id)
	if err != nil {
		return err
	}
	if err := fn(item); err != nil {
		return err
	}

	for _, child := range children[node.id] {
		if err := s.walk(ctx, child, children, visited, fn); err != nil {
			return err
		}
	}
	return nil
}

// loadNodes loads (id, parent_id, time_created) for every session.
func (s *Source) loadNodes(ctx context.Context) ([]sessionNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_id, time_created FROM session_v2`)
	if err != nil {
		return nil, fmt.Errorf("opencode: loading sessions: %w", err)
	}
	defer rows.Close()

	var nodes []sessionNode
	for rows.Next() {
		var n sessionNode
		var parentID sql.NullString
		if err := rows.Scan(&n.id, &parentID, &n.created); err != nil {
			return nil, fmt.Errorf("opencode: scanning session: %w", err)
		}
		n.parentID = parentID.String
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("opencode: reading sessions: %w", err)
	}
	return nodes, nil
}

// loadItem reads id's session_v2 row, streams its session_message rows
// in seq order into a translator, reads its todo rows in position order,
// and returns the resulting Item.
func (s *Source) loadItem(ctx context.Context, id string) (Item, error) {
	row, err := s.loadSessionRow(ctx, id)
	if err != nil {
		return Item{}, err
	}

	tr := newTranslator(row, s.agents)

	if err := s.streamMessages(ctx, id, tr); err != nil {
		return Item{}, err
	}

	todos, err := s.loadTodos(ctx, id)
	if err != nil {
		return Item{}, err
	}

	sess, msgs, stats := tr.finish()
	return Item{Session: sess, Messages: msgs, Todos: todos, Stats: stats}, nil
}

// loadSessionRow reads id's session_v2 row into a sessionRow.
func (s *Source) loadSessionRow(ctx context.Context, id string) (sessionRow, error) {
	var row sessionRow
	var parentID, title, agent, model sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, parent_id, directory, title, agent, model, time_created, time_updated
		 FROM session_v2 WHERE id = ?`, id,
	).Scan(&row.ID, &parentID, &row.Directory, &title, &agent, &model, &row.Created, &row.Updated)
	if err != nil {
		return sessionRow{}, fmt.Errorf("opencode: loading session %s: %w", id, err)
	}
	row.ParentID = parentID.String
	row.Title = title.String
	row.Agent = agent.String
	row.ModelJSON = model.String
	return row, nil
}

// streamMessages streams id's session_message rows in seq order with a
// row cursor and feeds each one to tr, so memory stays bounded to one
// session's worth of messages even on a 4+ GB database.
func (s *Source) streamMessages(ctx context.Context, id string, tr *translator) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, data, time_created FROM session_message WHERE session_id = ? ORDER BY seq`, id,
	)
	if err != nil {
		return fmt.Errorf("opencode: loading messages for %s: %w", id, err)
	}
	defer rows.Close()

	for rows.Next() {
		var m messageRow
		if err := rows.Scan(&m.ID, &m.Type, &m.Data, &m.Created); err != nil {
			return fmt.Errorf("opencode: scanning message for %s: %w", id, err)
		}
		tr.add(m)
	}
	return rows.Err()
}

// loadTodos reads id's todo rows in position order.
func (s *Source) loadTodos(ctx context.Context, id string) ([]core.Todo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT content, status FROM todo WHERE session_id = ? ORDER BY position`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("opencode: loading todos for %s: %w", id, err)
	}
	defer rows.Close()

	var todos []core.Todo
	for rows.Next() {
		var td core.Todo
		if err := rows.Scan(&td.Content, &td.Status); err != nil {
			return nil, fmt.Errorf("opencode: scanning todo for %s: %w", id, err)
		}
		todos = append(todos, td)
	}
	return todos, rows.Err()
}
