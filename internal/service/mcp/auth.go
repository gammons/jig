package mcp

import (
	"context"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// Authenticate runs the on-demand sign-in flow for name: it opens the
// Authenticator's gate, waits for the auth URL and publishes it, then
// reconnects with the gate held open (AuthOpen) until the flow ends. It
// blocks until the flow ends, so the TUI calls it inside a Cmd and
// `jig mcp auth` calls it directly.
func (m *Manager) Authenticate(ctx context.Context, name string) error {
	m.mu.Lock()
	st, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownServer
	}
	if st.cfg.Transport != core.MCPHTTP {
		m.mu.Unlock()
		return ErrNotHTTPServer
	}
	oldConn := st.conn
	m.mu.Unlock()

	if oldConn != nil {
		oldConn.Close()
	}

	gen, actx, cancel := beginGeneration(m, name)
	defer cancel()

	m.mu.Lock()
	st = m.servers[name]
	st.conn = nil
	st.tools = nil
	st.status.State = core.MCPAuthenticating
	st.status.Err = ""
	st.status.AuthURL = ""
	cfg := st.cfg
	notifyChangedLocked(m)
	m.mu.Unlock()
	publish(m, event.MCPServerChanged{Name: name, State: core.MCPAuthenticating})

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

	m.mu.Lock()
	st, ok = m.servers[name]
	if !ok || st.generation != gen {
		m.mu.Unlock()
		conn.Close()
		return nil
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
	m.mu.Unlock()
	publish(m, event.MCPServerChanged{Name: name, State: core.MCPReady, Tools: len(remote)})

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
