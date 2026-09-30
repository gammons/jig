// mcp.go wires the MCP Manager (internal/service/mcp) into the runtime:
// adapters onto client/mcp, client/mcpauth, and data/mcptokens, the
// headless warning printer, and the core.MCPService port.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	mcpclient "github.com/gammons/jig/internal/client/mcp"
	"github.com/gammons/jig/internal/client/mcpauth"
	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/data/config"
	"github.com/gammons/jig/internal/data/mcptokens"
	mcpsvc "github.com/gammons/jig/internal/service/mcp"
)

// authWaitTimeout bounds how long an interactive sign-in waits for the
// browser redirect (spec §6.2).
const authWaitTimeout = 5 * time.Minute

// newMCPManager builds a mcpsvc.Manager for e's resolved servers, or nil if
// there are none. The manager is not started; the caller calls Start once
// the registry is frozen, and Close it in rt.close().
func newMCPManager(e env, clk clock.Clock, bus *event.Bus, images mcpsvc.Imager) *mcpsvc.Manager {
	servers := resolvedMCPServers(e)
	if len(servers) == 0 {
		return nil
	}
	tokens := mcptokens.New(e.mcpAuthDir())
	adapters := newMCPAdapters(servers, e.workDir, tokens, clk)
	return mcpsvc.New(mcpsvc.Deps{
		Servers: servers, WorkDir: e.workDir,
		Dialer: dialerAdapter{adapters}, Auth: authAdapter{adapters}, Tokens: tokensAdapter{tokens},
		Clock: clk, Bus: bus, Images: images,
	})
}

// mcpServerState is one server's per-server auth state, shared between the
// dialerAdapter and the authAdapter.
type mcpServerState struct {
	gate *mcpauth.Gate

	mu          sync.Mutex
	pendingPort int
	pendingLn   *mcpauth.Listener
}

// mcpAdapters bundles the per-server state and collaborators the dialer
// and authenticator adapters share.
type mcpAdapters struct {
	mu      sync.Mutex
	states  map[string]*mcpServerState
	workDir string
	tokens  *mcptokens.Store
	clk     clock.Clock
}

// newMCPAdapters builds the shared per-server state for every server in
// servers: an http server with no static Authorization header gets a
// *mcpauth.Gate.
func newMCPAdapters(servers []core.MCPServer, workDir string, tokens *mcptokens.Store, clk clock.Clock) *mcpAdapters {
	a := &mcpAdapters{states: make(map[string]*mcpServerState, len(servers)), workDir: workDir, tokens: tokens, clk: clk}
	for _, srv := range servers {
		if needsGate(srv) {
			a.states[srv.Name] = &mcpServerState{gate: &mcpauth.Gate{}}
		}
	}
	return a
}

// needsGate reports whether srv is an http server with no static
// Authorization header (case-insensitive), so it may need an OAuth gate.
func needsGate(srv core.MCPServer) bool {
	if srv.Transport != core.MCPHTTP {
		return false
	}
	for k := range srv.Headers {
		if strings.EqualFold(k, "Authorization") {
			return false
		}
	}
	return true
}

// state returns the per-server state for name, or nil if it has none
// (stdio, sse, or an http server with a static Authorization header).
func (a *mcpAdapters) state(name string) *mcpServerState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.states[name]
}

// dialerAdapter implements mcpsvc.Dialer over mcpclient.Dial.
type dialerAdapter struct {
	adapters *mcpAdapters
}

// Dial builds a mcpclient.Spec from srv and connects. For an http server
// with a gate, it builds a fresh auth.AuthorizationCodeHandler, with
// RedirectPort from the stored record's port (AuthClosed) or the pending
// listener's port (AuthOpen).
func (d dialerAdapter) Dial(ctx context.Context, srv core.MCPServer, mode mcpsvc.AuthMode, onChanged func()) (mcpsvc.Conn, error) {
	spec := mcpclient.Spec{
		Name: srv.Name, Transport: srv.Transport,
		Command: srv.Command, Args: srv.Args,
		URL: srv.URL, Headers: srv.Headers,
		Version: "dev",
	}
	if srv.Transport == core.MCPStdio {
		spec = stdioSpec(srv, d.adapters.workDir, os.Environ())
	}
	if st := d.adapters.state(srv.Name); st != nil {
		h, err := d.buildAuthHandler(ctx, srv, st, mode)
		if err != nil {
			return nil, err
		}
		spec.OAuth = h
	}
	conn, err := mcpclient.Dial(ctx, spec, onChanged)
	if err != nil {
		return nil, err
	}
	return convertConn{conn}, nil
}

// buildAuthHandler builds the OAuth handler for srv, sized to the right
// redirect port for mode.
func (d dialerAdapter) buildAuthHandler(ctx context.Context, srv core.MCPServer, st *mcpServerState, mode mcpsvc.AuthMode) (sdkauth.OAuthHandler, error) {
	port := 0
	if mode == mcpsvc.AuthClosed {
		if rec, ok, _ := d.adapters.tokens.Load(srv.URL); ok {
			port = rec.RedirectPort
		}
	} else {
		st.mu.Lock()
		port = st.pendingPort
		st.mu.Unlock()
	}
	var pre *core.MCPOAuth
	if srv.OAuth.ClientID != "" {
		pre = &srv.OAuth
	}
	return mcpauth.NewHandler(ctx, mcpauth.Config{
		ServerURL: srv.URL, Pre: pre, Store: d.adapters.tokens,
		Gate: st.gate, HTTP: mcpauth.SecureClient(), RedirectPort: port,
	})
}

// stdioSpec builds the mcpclient.Spec for a stdio server: environ plus
// srv.Env (the server wins), and Dir from srv.Cwd (relative cwds joined to
// workDir), else workDir.
func stdioSpec(srv core.MCPServer, workDir string, environ []string) mcpclient.Spec {
	env := append([]string(nil), environ...)
	for k, v := range srv.Env {
		env = append(env, k+"="+v)
	}
	dir := workDir
	if srv.Cwd != "" {
		dir = srv.Cwd
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(workDir, dir)
		}
	}
	return mcpclient.Spec{
		Name: srv.Name, Transport: srv.Transport,
		Command: srv.Command, Args: srv.Args,
		Env: env, Dir: dir, Version: "dev",
	}
}

// convertConn adapts *mcpclient.Conn to mcpsvc.Conn.
type convertConn struct {
	c *mcpclient.Conn
}

func (c convertConn) ListTools(ctx context.Context) ([]mcpsvc.RemoteTool, error) {
	tools, err := c.c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpsvc.RemoteTool, len(tools))
	for i, t := range tools {
		out[i] = mcpsvc.RemoteTool{Name: t.Name, Description: t.Description, Schema: t.Schema, ReadOnly: t.ReadOnly}
	}
	return out, nil
}

func (c convertConn) CallTool(ctx context.Context, name string, args json.RawMessage) (mcpsvc.RemoteResult, error) {
	res, err := c.c.CallTool(ctx, name, args)
	if err != nil {
		return mcpsvc.RemoteResult{}, err
	}
	return convertRemote(res), nil
}

// convertRemote maps a client/mcp.CallResult into mcpsvc.RemoteResult.
func convertRemote(res mcpclient.CallResult) mcpsvc.RemoteResult {
	out := mcpsvc.RemoteResult{Structured: res.Structured, IsError: res.IsError}
	for _, c := range res.Content {
		out.Content = append(out.Content, mcpsvc.RemoteContent{
			Kind: c.Kind, Text: c.Text, MIME: c.MIME, URI: c.URI, Name: c.Name, Data: c.Data,
		})
	}
	return out
}

func (c convertConn) Close() error          { return c.c.Close() }
func (c convertConn) Done() <-chan struct{} { return c.c.Done() }
func (c convertConn) Err() error            { return c.c.Err() }

// authAdapter implements mcpsvc.Authenticator over the shared per-server
// state.
type authAdapter struct {
	adapters *mcpAdapters
}

// authSession implements mcpsvc.Session.
type authSession struct {
	st  *mcpServerState
	url chan string
}

func (s *authSession) URL() <-chan string { return s.url }

func (s *authSession) Close() {
	s.st.gate.Close()
	s.st.mu.Lock()
	if s.st.pendingLn != nil {
		s.st.pendingLn.Close()
		s.st.pendingLn = nil
	}
	s.st.pendingPort = 0
	s.st.mu.Unlock()
}

// Begin starts a callback listener, opens srv's gate with a fetch
// function that publishes the URL, opens the browser, and waits for the
// redirect (bounded by a 5-minute clock timeout).
//
// Begin has no exclusion of its own against a concurrent Begin for the same
// server: callers must reach it only through Manager.Authenticate, whose
// beginAuthenticate critical section rejects a second Authenticate while
// one is already in flight for that server.
func (a authAdapter) Begin(ctx context.Context, srv core.MCPServer) (mcpsvc.Session, error) {
	st := a.adapters.state(srv.Name)
	if st == nil {
		return nil, mcpsvc.ErrNotHTTPServer
	}
	storedPort := 0
	if rec, ok, _ := a.adapters.tokens.Load(srv.URL); ok {
		storedPort = rec.RedirectPort
	}
	ln, err := mcpauth.Listen(storedPort)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	st.pendingPort = ln.Port()
	st.pendingLn = ln
	st.mu.Unlock()

	sess := &authSession{st: st, url: make(chan string, 1)}
	st.gate.Open(func(fetchCtx context.Context, authURL string) (*sdkauth.AuthorizationResult, error) {
		select {
		case sess.url <- authURL:
		default:
		}
		_ = mcpauth.OpenBrowser(fetchCtx, authURL)
		return a.wait(fetchCtx, ln)
	})
	return sess, nil
}

// wait waits for ln's callback, bounded by a 5-minute clock timeout.
func (a authAdapter) wait(ctx context.Context, ln *mcpauth.Listener) (*sdkauth.AuthorizationResult, error) {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	var code, state, iss string
	var err error
	go func() {
		code, state, iss, err = ln.Wait(waitCtx)
		close(done)
	}()
	select {
	case <-done:
		if err != nil {
			return nil, err
		}
		return &sdkauth.AuthorizationResult{Code: code, State: state, Iss: iss}, nil
	case <-a.adapters.clk.After(authWaitTimeout):
		cancel()
		<-done
		return nil, fmt.Errorf("mcpauth: timed out waiting for sign-in")
	}
}

// tokensAdapter implements mcpsvc.Tokens over mcptokens.Store.
type tokensAdapter struct {
	store *mcptokens.Store
}

func (t tokensAdapter) Has(url string) bool {
	rec, ok, err := t.store.Load(url)
	return err == nil && ok && rec.Token != nil
}

func (t tokensAdapter) Delete(url string) error {
	return t.store.Delete(url)
}

// mcpPort implements core.MCPService over a *mcpsvc.Manager.
type mcpPort struct {
	mgr *mcpsvc.Manager
}

func (p mcpPort) Servers(context.Context) ([]core.MCPServerStatus, error) {
	return p.mgr.Servers(), nil
}

func (p mcpPort) Authenticate(ctx context.Context, name string) error {
	return p.mgr.Authenticate(ctx, name)
}

func (p mcpPort) CancelAuth(_ context.Context, name string) error {
	p.mgr.CancelAuth(name)
	return nil
}

func (p mcpPort) Logout(ctx context.Context, name string) error {
	return p.mgr.Logout(ctx, name)
}

func (p mcpPort) Reconnect(ctx context.Context, name string) error {
	return p.mgr.Reconnect(ctx, name)
}

// mcpWarnings returns the headless warning lines for servers that aren't
// ready: needs_auth and everything else get their own message.
func mcpWarnings(servers []core.MCPServerStatus) []string {
	var out []string
	for _, s := range servers {
		switch s.State {
		case core.MCPReady, core.MCPDisabled:
			continue
		case core.MCPNeedsAuth:
			out = append(out, fmt.Sprintf("mcp: %s needs sign-in; run \"jig mcp auth %s\"", s.Name, s.Name))
		case core.MCPConnecting:
			out = append(out, fmt.Sprintf("mcp: %s still connecting; continuing without it", s.Name))
		default:
			if s.Err == "" {
				out = append(out, fmt.Sprintf("mcp: %s failed", s.Name))
				continue
			}
			out = append(out, fmt.Sprintf("mcp: %s failed: %s", s.Name, s.Err))
		}
	}
	return out
}

// maxStartupTimeout is the maximum StartupTimeout among servers, defaulted
// to 10s per server when unset, used to bound headless's Settle call.
func maxStartupTimeout(servers []core.MCPServer) time.Duration {
	const def = 10 * time.Second
	max := def
	for _, s := range servers {
		d := s.StartupTimeout
		if d <= 0 {
			d = def
		}
		if d > max {
			max = d
		}
	}
	return max
}

// mcpToolSource returns mgr as an ext.ToolSource, or nil if mgr is nil (a
// nil *mcpsvc.Manager boxed in a non-nil interface would break the addTools
// nil check, so this stays explicit).
func mcpToolSource(mgr *mcpsvc.Manager) ext.ToolSource {
	if mgr == nil {
		return nil
	}
	return mgr
}
func resolvedMCPServers(e env) []core.MCPServer {
	return config.ResolvedMCP(e.cfg().MCP)
}
