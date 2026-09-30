package mcp

import (
	"context"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// beginAuthenticate validates name and, in one critical section, checks
// existence and http transport, bumps the generation (fencing off any
// earlier in-flight connect/auth and cancelling it), and sets
// MCPAuthenticating — all before the caller ever touches the old conn.
// Doing the generation bump and the state change atomically, before the
// old conn is closed, is what keeps the old conn's watchDone from
// mistaking its own supersession for a surprise drop (finding 1), and
// keeps a concurrent Reconnect from slipping into the window before
// MCPAuthenticating is visible (finding 2): Reconnect's own check runs
// under the same mutex.
func beginAuthenticate(m *Manager, name string) (oldConn Conn, gen int, ctx context.Context, cancel context.CancelFunc, cfg core.MCPServer, err error) {
	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return nil, 0, nil, nil, core.MCPServer{}, ErrUnknownServer
	}
	if st.cfg.Transport != core.MCPHTTP {
		m.mu.Unlock()
		return nil, 0, nil, nil, core.MCPServer{}, ErrNotHTTPServer
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
	st.status.State = core.MCPAuthenticating
	st.status.Err = ""
	st.status.AuthURL = ""
	cfg = st.cfg
	notifyChangedLocked(m)
	m.mu.Unlock()

	publish(m, event.MCPServerChanged{Name: name, State: core.MCPAuthenticating})
	return oldConn, gen, ctx, cancel, cfg, nil
}

// Authenticate runs the on-demand sign-in flow for name: it opens the
// Authenticator's gate, waits for the auth URL and publishes it, then
// reconnects with the gate held open (AuthOpen) until the flow ends. It
// blocks until the flow ends, so the TUI calls it inside a Cmd and
// `jig mcp auth` calls it directly.
func (m *Manager) Authenticate(ctx context.Context, name string) error {
	oldConn, gen, actx, cancel, cfg, err := beginAuthenticate(m, name)
	if err != nil {
		return err
	}
	defer cancel()

	if oldConn != nil {
		oldConn.Close()
	}

	sess, err := m.auth.Begin(actx, cfg)
	if err != nil {
		return finishAuthenticate(m, name, gen, actx, err)
	}
	defer sess.Close()

	go func() {
		select {
		case url, ok := <-sess.URL():
			if !ok {
				return
			}
			m.mu.Lock()
			st, ok := m.servers[name]
			if ok && st.generation == gen {
				st.status.AuthURL = url
				notifyChangedLocked(m)
			}
			m.mu.Unlock()
			publish(m, event.MCPServerChanged{Name: name, State: core.MCPAuthenticating})
		case <-actx.Done():
		}
	}()

	// Reconnect with the gate open, no startup timeout (bounded by the
	// fetcher's own 5-minute callback timeout).
	cfg.StartupTimeout = 0
	conn, err := m.dialer.Dial(actx, cfg, AuthOpen, buildOnChanged(m, name))
	if err != nil {
		return finishAuthenticate(m, name, gen, actx, err)
	}

	remote, err := conn.ListTools(actx)
	if err != nil {
		conn.Close()
		return finishAuthenticate(m, name, gen, actx, err)
	}

	e, applied := finishReady(m, name, gen, conn, remote)
	if !applied {
		conn.Close()
		return nil
	}
	publish(m, e)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		watchDone(m, name, conn, gen)
	}()
	return nil
}

// finishAuthenticate marks name failed after a sign-in error: "sign-in
// cancelled" if actx was cancelled (CancelAuth or Close), else the error's
// text. It returns the error the flow ends with (context.Canceled is
// still reported to the caller of Authenticate).
func finishAuthenticate(m *Manager, name string, gen int, actx context.Context, err error) error {
	msg := "sign-in cancelled"
	if actx.Err() == nil {
		msg = err.Error()
	}
	if e := setStateIfCurrent(m, name, gen, core.MCPFailed, msg); e.Name != "" {
		publish(m, e)
	}
	return err
}

// CancelAuth cancels name's in-progress Authenticate call, if any.
func (m *Manager) CancelAuth(name string) error {
	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownServer
	}
	cancel := st.cancel
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}
