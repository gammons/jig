// Package mcp owns every MCP server connection and its lifecycle state. It
// declares the small interfaces (Dialer, Conn, Authenticator, Tokens) that
// internal/app adapts in terms of client/mcp and data/mcptokens; this
// package never imports either. Manager implements ext.ToolSource and
// backs core.MCPService (the app wires the port's methods directly to it).
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
)

// defaultStartupTimeout and defaultToolTimeout are applied by Start to any
// server that doesn't set its own.
const (
	defaultStartupTimeout = 10 * time.Second
	defaultToolTimeout    = 2 * time.Minute
)

// AuthMode tells a Dialer whether the OAuth gate (if the server needs one)
// should be open (during Manager.Authenticate) or closed (every other
// connect).
type AuthMode int

const (
	AuthClosed AuthMode = iota
	AuthOpen
)

// Dialer connects to one MCP server. onChanged, if non-nil, is called
// whenever the server reports its tool list changed.
type Dialer interface {
	Dial(ctx context.Context, srv core.MCPServer, auth AuthMode, onChanged func()) (Conn, error)
}

// Conn is a live connection to one MCP server.
type Conn interface {
	ListTools(ctx context.Context) ([]RemoteTool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (RemoteResult, error)
	Close() error
	Done() <-chan struct{}
	Err() error
}

// RemoteTool is one tool a server advertises.
type RemoteTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ReadOnly    bool
}

// RemoteContent is one piece of a tool call result's content; it mirrors
// client/mcp.Content without depending on that package.
type RemoteContent struct {
	Kind string
	Text string
	MIME string
	URI  string
	Name string
	Data []byte
}

// RemoteResult is the outcome of a tools/call request.
type RemoteResult struct {
	Content    []RemoteContent
	Structured json.RawMessage
	IsError    bool
}

// Session is an in-progress sign-in: URL delivers the auth URL once the
// SDK has built it (at most once; it may never fire if the flow ends
// first), and Close releases whatever the Authenticator held for it.
type Session interface {
	URL() <-chan string
	Close()
}

// Authenticator opens the gate for one server's OAuth sign-in and returns
// a Session describing it. The Dialer used in AuthOpen mode shares the
// same gate; the app adapter wires them together per server.
type Authenticator interface {
	Begin(ctx context.Context, srv core.MCPServer) (Session, error)
}

// Tokens reports and clears stored OAuth tokens, keyed by server URL.
type Tokens interface {
	Has(url string) bool
	Delete(url string) error
}

// Imager decodes and stores image bytes as a bounded core.Media. The tool
// wrapper (tool.go / Task 8) uses it for image content; the Manager only
// threads it through from Deps.
type Imager interface {
	Process(data []byte) (core.Media, core.ImageInfo, error)
}

// ErrUnknownServer is returned by any Manager method naming a server that
// wasn't in Deps.Servers.
var ErrUnknownServer = errors.New("mcp: unknown server")

// ErrNotHTTPServer is returned by Authenticate for a server that isn't an
// http transport (stdio and sse servers never need the OAuth flow).
var ErrNotHTTPServer = errors.New("mcp: server does not use http transport")

// ErrAuthenticating is returned by Reconnect for a server that is
// currently running Authenticate.
var ErrAuthenticating = errors.New("mcp: server is authenticating")

// Deps are the Manager's collaborators.
type Deps struct {
	Servers []core.MCPServer
	WorkDir string

	Dialer Dialer
	Auth   Authenticator
	Tokens Tokens

	Clock clock.Clock
	Bus   event.Publisher

	Images Imager
}

// serverState is one server's mutable state, guarded by Manager.mu.
type serverState struct {
	cfg    core.MCPServer
	status core.MCPServerStatus

	conn   Conn
	remote []RemoteTool
	tools  []ext.Tool

	// generation counts intentional conn replacements (a fresh connect,
	// Authenticate, Logout, Reconnect, or Close). watchDone and any
	// background relist compare it against the value captured when they
	// started, so a conn the Manager itself superseded or closed never
	// gets mistaken for a surprise drop.
	generation int

	// cancel cancels the in-flight dial or auth operation for this
	// server, if any.
	cancel context.CancelFunc
}

// Manager owns every MCP server connection and state. Its mutex is the one
// owner of every serverState; every state change publishes
// event.MCPServerChanged after the lock is released.
type Manager struct {
	mu sync.Mutex

	// changed is closed and replaced under mu every time a server's
	// status changes, so Settle can wait on it without polling or a
	// leaked goroutine.
	changed chan struct{}

	servers map[string]*serverState

	dialer Dialer
	auth   Authenticator
	tokens Tokens
	clk    clock.Clock
	bus    event.Publisher
	images Imager

	workDir string

	wg sync.WaitGroup

	baseCtx    context.Context
	baseCancel context.CancelFunc
	closed     bool
}

// New builds a Manager for the given servers. It does nothing else until
// Start is called.
func New(deps Deps) *Manager {
	m := &Manager{
		servers: make(map[string]*serverState, len(deps.Servers)),
		changed: make(chan struct{}),
		dialer:  deps.Dialer,
		auth:    deps.Auth,
		tokens:  deps.Tokens,
		clk:     deps.Clock,
		bus:     deps.Bus,
		images:  deps.Images,
		workDir: deps.WorkDir,
	}
	for _, cfg := range deps.Servers {
		m.servers[cfg.Name] = &serverState{
			cfg: cfg,
			status: core.MCPServerStatus{
				Name:      cfg.Name,
				Source:    cfg.Source,
				Transport: cfg.Transport,
			},
		}
	}
	m.baseCtx, m.baseCancel = context.WithCancel(context.Background())
	return m
}

// Start applies defaults (a 10s startup timeout, a 2m tool timeout, and
// Cwd = WorkDir when empty), marks disabled servers, and connects every
// other server in the background. It returns immediately.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.baseCtx, m.baseCancel = context.WithCancel(ctx)
	var toConnect []string
	for name, st := range m.servers {
		cfg := st.cfg
		if cfg.StartupTimeout <= 0 {
			cfg.StartupTimeout = defaultStartupTimeout
		}
		if cfg.ToolTimeout <= 0 {
			cfg.ToolTimeout = defaultToolTimeout
		}
		if cfg.Cwd == "" {
			cfg.Cwd = m.workDir
		}
		st.cfg = cfg

		if cfg.Enabled != nil && !*cfg.Enabled {
			st.status.State = core.MCPDisabled
			continue
		}
		st.status.State = core.MCPConnecting
		toConnect = append(toConnect, name)
	}
	m.mu.Unlock()

	for _, name := range toConnect {
		m.wg.Add(1)
		go m.connectServer(name)
	}
}

// Tools implements ext.ToolSource: a fresh slice of every ready server's
// wrapped tools, ordered by server name then tool name. It does no I/O.
func (m *Manager) Tools() []ext.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	var out []ext.Tool
	for _, name := range sortedNames(m.servers) {
		st := m.servers[name]
		if st.status.State != core.MCPReady {
			continue
		}
		out = append(out, append([]ext.Tool(nil), st.tools...)...)
	}
	return out
}

// Servers reports every server's current status, in name order.
func (m *Manager) Servers() []core.MCPServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]core.MCPServerStatus, 0, len(m.servers))
	for _, name := range sortedNames(m.servers) {
		st := m.servers[name]
		s := st.status
		s.ToolNames = append([]string(nil), s.ToolNames...)
		if m.tokens != nil && (st.cfg.Transport == core.MCPHTTP || st.cfg.Transport == core.MCPSSE) {
			s.HasToken = m.tokens.Has(st.cfg.URL)
		}
		out = append(out, s)
	}
	return out
}

// liveConn returns the current Conn for name, only while it is ready.
func (m *Manager) liveConn(name string) (Conn, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok || st.status.State != core.MCPReady || st.conn == nil {
		return nil, false
	}
	return st.conn, true
}

// Close cancels every in-flight dial or auth operation, closes every live
// connection, and waits for the Manager's goroutines to exit (bounded,
// since every one of them observes either a closed conn or baseCtx's
// cancellation). After Close, Tools is empty.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.baseCancel != nil {
		m.baseCancel()
	}
	var conns []Conn
	for _, st := range m.servers {
		if st.conn != nil {
			st.generation++
			conns = append(conns, st.conn)
			st.conn = nil
			st.tools = nil
		}
		if st.cancel != nil {
			st.cancel()
		}
	}
	notifyChangedLocked(m)
	m.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
	m.wg.Wait()
	return nil
}
