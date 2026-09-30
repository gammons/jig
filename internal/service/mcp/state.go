package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// sortStrings sorts ss ascending. A tiny wrapper so manager.go's one
// caller doesn't need its own "sort" import line.
func sortStrings(ss []string) {
	sort.Strings(ss)
}

// sortedNames returns m's server names in ascending order.
func sortedNames(servers map[string]*serverState) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// notifyChangedLocked wakes every Settle waiter. Callers must hold m.mu.
func notifyChangedLocked(m *Manager) {
	close(m.changed)
	m.changed = make(chan struct{})
}

// publish sends e on the bus, if one is configured. Callers must never
// hold m.mu here: every event is published after the lock is released.
func publish(m *Manager, e event.Event) {
	if m.bus != nil {
		m.bus.Publish(e)
	}
}

// beginGeneration bumps name's generation (fencing off any earlier
// in-flight connect/auth attempt for it) and returns the new value along
// with a fresh cancellable context derived from baseCtx. Callers must hold
// no lock; it takes and releases m.mu itself.
func beginGeneration(m *Manager, name string) (gen int, ctx context.Context, cancel context.CancelFunc) {
	m.mu.Lock()
	st := m.servers[name]
	st.generation++
	gen = st.generation
	if st.cancel != nil {
		st.cancel()
	}
	parent := m.baseCtx
	m.mu.Unlock()

	ctx, cancel = context.WithCancel(parent)
	m.mu.Lock()
	st.cancel = cancel
	m.mu.Unlock()
	return gen, ctx, cancel
}

// connectServer runs one server's connect-and-watch lifecycle from a
// freshly bumped generation. It is started as a goroutine by Start.
func (m *Manager) connectServer(name string) {
	defer m.wg.Done()
	gen, ctx, cancel := beginGeneration(m, name)
	connectAndWatch(m, name, gen, ctx, cancel, AuthClosed)
}

// finishReady installs conn and remote as name's live, ready connection,
// but only while gen is still current. It returns the ready event to
// publish and whether it applied; on false the caller must close conn
// itself (a newer attempt already owns the server's conn slot). Shared by
// connectAndWatch and Authenticate so both "install a fresh ready
// connection" paths behave identically.
func finishReady(m *Manager, name string, gen int, conn Conn, remote []RemoteTool) (event.MCPServerChanged, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok || st.generation != gen {
		return event.MCPServerChanged{}, false
	}
	st.conn = conn
	st.remote = remote
	st.tools = buildTools(m, name, remote)
	st.status.State = core.MCPReady
	st.status.Err = ""
	st.status.AuthURL = ""
	st.status.Tools = len(st.tools)
	st.status.ToolNames = toolNames(remote)
	notifyChangedLocked(m)
	return event.MCPServerChanged{Name: name, State: core.MCPReady, Tools: len(remote)}, true
}

// connectAndWatch dials name under ctx (already fenced by gen), lists its
// tools on success, and then watches its Done channel until the Manager
// itself supersedes the connection (a higher generation). Every state
// update it makes is guarded by a check that gen is still current, so a
// superseded attempt (Reconnect, Logout, Authenticate, or Close) can never
// clobber newer state.
func connectAndWatch(m *Manager, name string, gen int, ctx context.Context, cancel context.CancelFunc, auth AuthMode) {
	m.mu.Lock()
	cfg := m.servers[name].cfg
	m.mu.Unlock()

	conn, err := dial(m, ctx, cancel, cfg, auth, name)
	clearCancelIfCurrent(m, name, gen)
	if err != nil {
		if !stillCurrent(m, name, gen) {
			return
		}
		var e event.MCPServerChanged
		if errors.Is(err, core.ErrMCPNeedsAuth) {
			e = setStateIfCurrent(m, name, gen, core.MCPNeedsAuth, "")
		} else {
			e = setStateIfCurrent(m, name, gen, core.MCPFailed, err.Error())
		}
		if e.Name != "" {
			publish(m, e)
		}
		return
	}

	remote, err := conn.ListTools(ctx)
	if err != nil || !stillCurrent(m, name, gen) {
		conn.Close()
		if err == nil {
			return // superseded; the newer attempt owns the conn slot
		}
		if e := setStateIfCurrent(m, name, gen, core.MCPFailed, err.Error()); e.Name != "" {
			publish(m, e)
		}
		return
	}

	e, applied := finishReady(m, name, gen, conn, remote)
	if !applied {
		conn.Close()
		return
	}
	publish(m, e)

	watchDone(m, name, conn, gen)
}

// dial races the Dialer against the clock's startup timeout (skipped when
// cfg.StartupTimeout is zero, as Authenticate's reconnect does). On
// timeout, it cancels ctx and, if the dial eventually returns a conn
// anyway, closes it rather than blocking on it.
func dial(m *Manager, ctx context.Context, cancel context.CancelFunc, cfg core.MCPServer, auth AuthMode, name string) (Conn, error) {
	if cfg.StartupTimeout <= 0 {
		return m.dialer.Dial(ctx, cfg, auth, buildOnChanged(m, name))
	}

	type result struct {
		conn Conn
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		conn, err := m.dialer.Dial(ctx, cfg, auth, buildOnChanged(m, name))
		resCh <- result{conn, err}
	}()

	select {
	case r := <-resCh:
		return r.conn, r.err
	case <-m.clk.After(cfg.StartupTimeout):
		cancel()
		go func() {
			r := <-resCh
			if r.conn != nil {
				r.conn.Close()
			}
		}()
		return nil, fmt.Errorf("timed out after %s", cfg.StartupTimeout)
	}
}

// stillCurrent reports whether gen is still name's generation.
func stillCurrent(m *Manager, name string, gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	return ok && st.generation == gen
}

// clearCancelIfCurrent clears name's cancel func, but only while gen is
// still current (a superseding operation already replaced it).
func clearCancelIfCurrent(m *Manager, name string, gen int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if ok && st.generation == gen {
		st.cancel = nil
	}
}

// setStateIfCurrent updates name's status, but only while gen is still
// current. It returns the zero event (Name == "") when it did nothing.
func setStateIfCurrent(m *Manager, name string, gen int, state core.MCPState, errMsg string) event.MCPServerChanged {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok || st.generation != gen {
		return event.MCPServerChanged{}
	}
	st.status.State = state
	st.status.Err = errMsg
	if state != core.MCPAuthenticating {
		st.status.AuthURL = ""
	}
	notifyChangedLocked(m)
	return event.MCPServerChanged{Name: name, State: state, Tools: st.status.Tools, Err: errMsg}
}

// buildOnChanged returns the callback the Dialer invokes when the server's
// tool list changes: it re-lists (best-effort) and publishes on success,
// unless the server has since moved on to a different connection.
func buildOnChanged(m *Manager, name string) func() {
	return func() {
		m.mu.Lock()
		st, ok := m.servers[name]
		if !ok || st.status.State != core.MCPReady || st.conn == nil {
			m.mu.Unlock()
			return
		}
		conn := st.conn
		gen := st.generation
		m.mu.Unlock()

		remote, err := conn.ListTools(context.Background())
		if err != nil {
			return
		}

		m.mu.Lock()
		st, ok = m.servers[name]
		if !ok || st.generation != gen {
			m.mu.Unlock()
			return
		}
		st.remote = remote
		st.tools = buildTools(m, name, remote)
		st.status.Tools = len(st.tools)
		st.status.ToolNames = toolNames(remote)
		notifyChangedLocked(m)
		m.mu.Unlock()
		publish(m, event.MCPServerChanged{Name: name, State: core.MCPReady, Tools: len(remote)})
	}
}

// watchDone blocks until conn's Done channel closes. If the server's
// generation hasn't moved on (i.e. the Manager didn't itself supersede or
// close this conn), it moves the server to failed with conn.Err().
func watchDone(m *Manager, name string, conn Conn, gen int) {
	<-conn.Done()

	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok || st.generation != gen {
		m.mu.Unlock()
		return
	}
	err := conn.Err()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	st.status.State = core.MCPFailed
	st.status.Err = msg
	st.conn = nil
	st.tools = nil
	notifyChangedLocked(m)
	m.mu.Unlock()
	publish(m, event.MCPServerChanged{Name: name, State: core.MCPFailed, Err: msg})
}

// markFailed moves name to failed with err's message and publishes the
// change, but only while gen is still current (finding 4): a CallTool
// failure captured against a stale (conn, gen) pair must never clobber a
// newer connection that a concurrent Reconnect, Logout, or Authenticate
// has since installed.
func (m *Manager) markFailed(name string, gen int, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if e := setStateIfCurrent(m, name, gen, core.MCPFailed, msg); e.Name != "" {
		publish(m, e)
	}
}

// toolNames builds core.MCPServerStatus.ToolNames: "<name> — <description>",
// each capped at 120 runes, sorted.
func toolNames(remote []RemoteTool) []string {
	out := make([]string, 0, len(remote))
	for _, rt := range remote {
		s := rt.Name + " — " + rt.Description
		out = append(out, capRunes(s, 120))
	}
	sort.Strings(out)
	return out
}

// capRunes truncates s to at most n runes.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Settle blocks until every server has left connecting, or d passes on the
// Manager's clock.
func (m *Manager) Settle(ctx context.Context, d time.Duration) {
	deadline := m.clk.After(d)
	for {
		m.mu.Lock()
		settled := true
		for _, st := range m.servers {
			if st.status.State == core.MCPConnecting {
				settled = false
				break
			}
		}
		ch := m.changed
		m.mu.Unlock()
		if settled {
			return
		}
		select {
		case <-ch:
		case <-deadline:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Reconnect closes name's current connection (if any) and dials again.
// Allowed in any state except authenticating: the check and the
// generation bump happen in one critical section, so a Reconnect can never
// slip into the window before Authenticate's MCPAuthenticating becomes
// visible (finding 2).
func (m *Manager) Reconnect(ctx context.Context, name string) error {
	oldConn, gen, dctx, cancel, err := beginReconnect(m, name)
	if err != nil {
		return err
	}

	if oldConn != nil {
		oldConn.Close()
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		connectAndWatch(m, name, gen, dctx, cancel, AuthClosed)
	}()
	return nil
}

// beginReconnect validates name and, in one critical section, rejects an
// authenticating server, then bumps the generation (cancelling any prior
// in-flight op) and sets MCPConnecting.
func beginReconnect(m *Manager, name string) (oldConn Conn, gen int, ctx context.Context, cancel context.CancelFunc, err error) {
	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return nil, 0, nil, nil, ErrUnknownServer
	}
	if st.status.State == core.MCPAuthenticating {
		m.mu.Unlock()
		return nil, 0, nil, nil, ErrAuthenticating
	}
	oldConn = st.conn
	if st.cancel != nil {
		st.cancel()
	}
	st.generation++
	gen = st.generation
	ctx, cancel = context.WithCancel(m.baseCtx)
	st.cancel = cancel
	st.conn = nil
	st.tools = nil
	st.status.State = core.MCPConnecting
	st.status.Err = ""
	notifyChangedLocked(m)
	m.mu.Unlock()

	publish(m, event.MCPServerChanged{Name: name, State: core.MCPConnecting})
	return oldConn, gen, ctx, cancel, nil
}

// Logout deletes name's stored token, then closes and reconnects it with
// the OAuth gate closed. The server ends needs_auth if it requires auth.
func (m *Manager) Logout(ctx context.Context, name string) error {
	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownServer
	}
	url := st.cfg.URL
	m.mu.Unlock()

	if m.tokens != nil {
		if err := m.tokens.Delete(url); err != nil {
			return err
		}
	}
	return m.Reconnect(ctx, name)
}
