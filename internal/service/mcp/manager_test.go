package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

const testTimeout = 5 * time.Second

func boolPtr(b bool) *bool { return &b }

func httpServer(name, url string) core.MCPServer {
	return core.MCPServer{Name: name, Transport: core.MCPHTTP, URL: url}
}

func stdioServer(name string) core.MCPServer {
	return core.MCPServer{Name: name, Transport: core.MCPStdio, Command: "somecmd"}
}

// harness bundles a Manager with its fakes for the tests in this file.
type harness struct {
	t      *testing.T
	m      *Manager
	dialer *fakeDialer
	auth   *fakeAuth
	tokens *fakeTokens
	clk    *clock.Fake
	rec    *recorder
}

func newHarness(t *testing.T, servers ...core.MCPServer) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		dialer: newFakeDialer(),
		auth:   &fakeAuth{},
		tokens: newFakeTokens(),
		clk:    clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		rec:    newRecorder(),
	}
	h.m = New(Deps{
		Servers: servers,
		WorkDir: "/work",
		Dialer:  h.dialer,
		Auth:    h.auth,
		Tokens:  h.tokens,
		Clock:   h.clk,
		Bus:     h.rec,
	})
	t.Cleanup(func() { h.m.Close() })
	return h
}

func (h *harness) waitState(name string, state core.MCPState) core.MCPServerStatus {
	h.t.Helper()
	ok := h.rec.waitFor(h.t, testTimeout, func(evs []event.Event) bool {
		for _, e := range evs {
			if c, ok := e.(event.MCPServerChanged); ok && c.Name == name && c.State == state {
				return true
			}
		}
		return false
	})
	if !ok {
		h.t.Fatalf("server %q never published state %q (last status: %+v, events: %+v)", name, state, statusOf(h.m, name), h.rec.changesFor(name))
	}
	return statusOf(h.m, name)
}

func statusOf(m *Manager, name string) core.MCPServerStatus {
	for _, s := range m.Servers() {
		if s.Name == name {
			return s
		}
	}
	return core.MCPServerStatus{}
}

func TestManager_StartStates(t *testing.T) {
	readyConn := newFakeConn()
	readyConn.setTools([]RemoteTool{{Name: "get_issue", Description: "gets an issue"}})

	h := newHarness(t,
		httpServer("ready", "https://ready.example/mcp"),
		httpServer("auth", "https://auth.example/mcp"),
		stdioServer("broken"),
		func() core.MCPServer { s := stdioServer("off"); s.Enabled = boolPtr(false); return s }(),
	)
	h.dialer.script("ready", fakeDial{conn: readyConn})
	h.dialer.script("auth", fakeDial{err: core.ErrMCPNeedsAuth})
	h.dialer.script("broken", fakeDial{err: errors.New("boom")})

	h.m.Start(context.Background())

	ready := h.waitState("ready", core.MCPReady)
	if ready.Tools != 1 {
		t.Errorf("ready.Tools = %d, want 1", ready.Tools)
	}
	wantNames := []string{"get_issue — gets an issue"}
	if len(ready.ToolNames) != 1 || ready.ToolNames[0] != wantNames[0] {
		t.Errorf("ready.ToolNames = %v, want %v", ready.ToolNames, wantNames)
	}

	h.waitState("auth", core.MCPNeedsAuth)
	failed := h.waitState("broken", core.MCPFailed)
	if failed.Err != "boom" {
		t.Errorf("broken.Err = %q, want %q", failed.Err, "boom")
	}

	off := statusOf(h.m, "off")
	if off.State != core.MCPDisabled {
		t.Errorf("off.State = %q, want disabled", off.State)
	}

	// Events arrive in order per server: connecting isn't itself
	// published (Start sets it synchronously before any goroutine runs),
	// so each server gets exactly one MCPServerChanged, to its final
	// state. Every event on the bus is one of those state changes (the
	// Manager never publishes anything else).
	for _, name := range []string{"ready", "auth", "broken"} {
		changes := h.rec.changesFor(name)
		if len(changes) == 0 {
			t.Errorf("%s: no events published", name)
		}
	}
	for _, e := range h.rec.all() {
		if _, ok := e.(event.MCPServerChanged); !ok {
			t.Errorf("unexpected event type on the bus: %#v", e)
		}
	}
}

func TestManager_StartupTimeout(t *testing.T) {
	srv := httpServer("slow", "https://slow.example/mcp")
	srv.StartupTimeout = 10 * time.Second
	h := newHarness(t, srv)
	started := make(chan struct{})
	h.dialer.script("slow", fakeDial{block: true, started: started})

	h.m.Start(context.Background())
	<-started
	h.clk.BlockUntilWaiters(1)
	h.clk.Advance(10 * time.Second)

	got := h.waitState("slow", core.MCPFailed)
	if got.Err != "timed out after 10s" {
		t.Errorf("Err = %q, want %q", got.Err, "timed out after 10s")
	}
}

func TestManager_ListChanged(t *testing.T) {
	conn := newFakeConn()
	conn.setTools([]RemoteTool{{Name: "one"}})
	h := newHarness(t, httpServer("srv", "https://x/mcp"))
	h.dialer.script("srv", fakeDial{conn: conn})

	h.m.Start(context.Background())
	h.waitState("srv", core.MCPReady)

	conn.setTools([]RemoteTool{{Name: "one"}, {Name: "two"}})
	conn.fireChanged()

	ok := h.rec.waitFor(t, testTimeout, func(evs []event.Event) bool {
		return statusOf(h.m, "srv").Tools == 2
	})
	if !ok {
		t.Fatalf("tool count never reached 2 (status: %+v)", statusOf(h.m, "srv"))
	}

	tools := h.m.Tools()
	if len(tools) != 2 {
		t.Fatalf("Tools() = %d, want 2", len(tools))
	}
}

func TestManager_ConnDoneFails(t *testing.T) {
	conn := newFakeConn()
	h := newHarness(t, httpServer("srv", "https://x/mcp"))
	h.dialer.script("srv", fakeDial{conn: conn})

	h.m.Start(context.Background())
	h.waitState("srv", core.MCPReady)

	conn.dropWithErr(errors.New("connection reset"))

	got := h.waitState("srv", core.MCPFailed)
	if got.Err != "connection reset" {
		t.Errorf("Err = %q, want %q", got.Err, "connection reset")
	}
}

func TestManager_ReconnectFromFailed(t *testing.T) {
	h := newHarness(t, stdioServer("srv"))
	h.dialer.script("srv", fakeDial{err: errors.New("boom")})
	h.m.Start(context.Background())
	h.waitState("srv", core.MCPFailed)

	conn2 := newFakeConn()
	conn2.setTools([]RemoteTool{{Name: "ok"}})
	h.dialer.script("srv", fakeDial{conn: conn2})

	if err := h.m.Reconnect(context.Background(), "srv"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	got := h.waitState("srv", core.MCPReady)
	if got.Tools != 1 {
		t.Errorf("Tools = %d, want 1", got.Tools)
	}
}

func TestManager_ToolsReturnsCopy(t *testing.T) {
	conn := newFakeConn()
	conn.setTools([]RemoteTool{{Name: "one"}})
	h := newHarness(t, httpServer("srv", "https://x/mcp"))
	h.dialer.script("srv", fakeDial{conn: conn})
	h.m.Start(context.Background())
	h.waitState("srv", core.MCPReady)

	first := h.m.Tools()
	if len(first) != 1 {
		t.Fatalf("Tools() = %d, want 1", len(first))
	}
	first[0] = nil

	second := h.m.Tools()
	if second[0] == nil {
		t.Fatalf("mutating the first slice affected the second")
	}
}

func TestManager_Settle(t *testing.T) {
	h := newHarness(t, httpServer("srv", "https://x/mcp"))
	started := make(chan struct{})
	h.dialer.script("srv", fakeDial{block: true, started: started})
	h.m.Start(context.Background())
	<-started

	done := make(chan struct{})
	go func() {
		h.m.Settle(context.Background(), time.Second)
		close(done)
	}()

	h.clk.BlockUntilWaiters(2) // one for the startup timeout, one for Settle's deadline
	h.clk.Advance(time.Second)

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Settle never returned")
	}
}

func TestManager_Settle_ReturnsWhenReady(t *testing.T) {
	conn := newFakeConn()
	h := newHarness(t, httpServer("srv", "https://x/mcp"))
	h.dialer.script("srv", fakeDial{conn: conn})
	h.m.Start(context.Background())

	done := make(chan struct{})
	go func() {
		h.m.Settle(context.Background(), time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Settle never returned once the server was ready")
	}
}

func TestManager_AuthenticateFlow(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	sess := newFakeSession()
	h.auth.sess = sess
	conn := newFakeConn()
	conn.setTools([]RemoteTool{{Name: "get_issue"}})
	release := make(chan struct{})
	h.dialer.script("gh", fakeDial{conn: conn, release: release})

	done := make(chan error, 1)
	go func() { done <- h.m.Authenticate(context.Background(), "gh") }()

	h.waitState("gh", core.MCPAuthenticating)
	sess.urlCh <- "https://auth.example/authorize"

	ok := h.rec.waitFor(t, testTimeout, func(evs []event.Event) bool {
		return statusOf(h.m, "gh").AuthURL == "https://auth.example/authorize"
	})
	if !ok {
		t.Fatalf("AuthURL never set (status: %+v)", statusOf(h.m, "gh"))
	}
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("Authenticate never returned")
	}

	got := statusOf(h.m, "gh")
	if got.State != core.MCPReady {
		t.Errorf("State = %q, want ready", got.State)
	}

	modes := h.dialer.authModes("gh")
	opens := 0
	for _, m := range modes {
		if m == AuthOpen {
			opens++
		}
	}
	if opens != 1 {
		t.Errorf("AuthOpen dials = %d, want 1 (modes: %v)", opens, modes)
	}
}

// TestManager_AuthenticateRightAfterStart reproduces `jig mcp auth`: Start
// then Authenticate at once. Start's background connect must not be able
// to supersede (and cancel) the sign-in, even when its goroutine only gets
// to run after Authenticate has begun.
func TestManager_AuthenticateRightAfterStart(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	sess := newFakeSession()
	h.auth.sess = sess
	startDial := make(chan struct{})
	h.auth.waitBefore = startDial
	h.dialer.script("gh", fakeDial{block: true, started: startDial})
	h.dialer.script("gh", fakeDial{})

	h.m.Start(context.Background())
	err := h.m.Authenticate(context.Background(), "gh")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got := statusOf(h.m, "gh"); got.State != core.MCPReady {
		t.Errorf("State = %q (err %q), want ready", got.State, got.Err)
	}
}

func TestManager_AuthenticateCancel(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	sess := newFakeSession()
	h.auth.sess = sess
	started := make(chan struct{})
	h.dialer.script("gh", fakeDial{block: true, started: started})

	done := make(chan error, 1)
	go func() { done <- h.m.Authenticate(context.Background(), "gh") }()

	h.waitState("gh", core.MCPAuthenticating)
	<-started
	if err := h.m.CancelAuth("gh"); err != nil {
		t.Fatalf("CancelAuth: %v", err)
	}

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Authenticate never returned after CancelAuth")
	}

	got := h.waitState("gh", core.MCPFailed)
	if got.Err != "sign-in cancelled" {
		t.Errorf("Err = %q, want %q", got.Err, "sign-in cancelled")
	}
}

func TestManager_AuthenticateStdioRejected(t *testing.T) {
	h := newHarness(t, stdioServer("srv"))
	h.dialer.script("srv", fakeDial{err: errors.New("boom")})
	h.m.Start(context.Background())
	h.waitState("srv", core.MCPFailed)

	err := h.m.Authenticate(context.Background(), "srv")
	if !errors.Is(err, ErrNotHTTPServer) {
		t.Errorf("Authenticate = %v, want ErrNotHTTPServer", err)
	}
}

func TestManager_AuthenticateUnknownServer(t *testing.T) {
	h := newHarness(t)
	err := h.m.Authenticate(context.Background(), "nope")
	if !errors.Is(err, ErrUnknownServer) {
		t.Errorf("Authenticate = %v, want ErrUnknownServer", err)
	}
}

func TestManager_Logout(t *testing.T) {
	conn := newFakeConn()
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	h.dialer.script("gh", fakeDial{conn: conn})
	h.m.Start(context.Background())
	h.waitState("gh", core.MCPReady)

	h.dialer.script("gh", fakeDial{err: core.ErrMCPNeedsAuth})
	if err := h.m.Logout(context.Background(), "gh"); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	deleted := h.tokens.deletedURLs()
	if len(deleted) != 1 || deleted[0] != "https://gh.example/mcp" {
		t.Errorf("deleted = %v, want [https://gh.example/mcp]", deleted)
	}

	h.waitState("gh", core.MCPNeedsAuth)
	if !conn.isClosed() {
		t.Errorf("old conn was never closed")
	}
}

func TestManager_ShadowedURLNoToken(t *testing.T) {
	global := httpServer("gh", "https://global.example/mcp")
	h := newHarness(t, global)
	h.tokens.has["https://global.example/mcp"] = true
	// A project layer's "gh" would have replaced Deps.Servers before New,
	// so this Manager only ever sees the project URL: Tokens.Has must be
	// queried with that URL only, never the shadowed global one.
	h.m = New(Deps{
		Servers: []core.MCPServer{httpServer("gh", "https://project.example/mcp")},
		Dialer:  h.dialer, Auth: h.auth, Tokens: h.tokens, Clock: h.clk, Bus: h.rec,
	})

	statuses := h.m.Servers()
	if len(statuses) != 1 || statuses[0].HasToken {
		t.Errorf("Servers() = %+v, want HasToken false for the project URL", statuses)
	}
}

func TestManager_CloseClosesAll(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	h := newHarness(t, httpServer("a", "https://a/mcp"), httpServer("b", "https://b/mcp"))
	h.dialer.script("a", fakeDial{conn: conn1})
	h.dialer.script("b", fakeDial{conn: conn2})
	h.m.Start(context.Background())
	h.waitState("a", core.MCPReady)
	h.waitState("b", core.MCPReady)

	if err := h.m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !conn1.isClosed() || !conn2.isClosed() {
		t.Errorf("conn1.closed=%v conn2.closed=%v, want both true", conn1.isClosed(), conn2.isClosed())
	}
	if tools := h.m.Tools(); len(tools) != 0 {
		t.Errorf("Tools() after Close = %d, want 0", len(tools))
	}
}

func TestManager_CloseUnblocksPendingStartupTimeout(t *testing.T) {
	h := newHarness(t, httpServer("slow", "https://slow/mcp"))
	started := make(chan struct{})
	h.dialer.script("slow", fakeDial{block: true, started: started})
	h.m.Start(context.Background())
	<-started

	done := make(chan struct{})
	go func() {
		h.m.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Close never returned while a dial was still blocked")
	}
}

// TestManager_ReconnectRejectedWhileAuthenticating pins finding 2:
// Reconnect must not slip into the window before Authenticate's
// MCPAuthenticating state becomes visible, and must stay rejected for as
// long as the server remains in that state.
func TestManager_ReconnectRejectedWhileAuthenticating(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	sess := newFakeSession()
	h.auth.sess = sess
	started := make(chan struct{})
	h.dialer.script("gh", fakeDial{block: true, started: started})

	done := make(chan error, 1)
	go func() { done <- h.m.Authenticate(context.Background(), "gh") }()

	h.waitState("gh", core.MCPAuthenticating)
	<-started

	before := len(h.rec.all())
	if err := h.m.Reconnect(context.Background(), "gh"); !errors.Is(err, ErrAuthenticating) {
		t.Fatalf("Reconnect = %v, want ErrAuthenticating", err)
	}
	if after := len(h.rec.all()); after != before {
		t.Errorf("Reconnect published %d extra events, want 0", after-before)
	}

	if err := h.m.CancelAuth("gh"); err != nil {
		t.Fatalf("CancelAuth: %v", err)
	}
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Authenticate never returned after CancelAuth")
	}
}

// TestManager_AuthenticateNoSpuriousFailed pins finding 1: Authenticate
// must not let the old conn's watchDone publish a spurious "failed" event
// partway through a successful sign-in. It starts a server ready with a
// live conn, runs Authenticate to completion, and asserts the server's
// full event sequence never contains "failed".
func TestManager_AuthenticateNoSpuriousFailed(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))
	oldConn := newFakeConn()
	h.dialer.script("gh", fakeDial{conn: oldConn})
	h.m.Start(context.Background())
	h.waitState("gh", core.MCPReady)

	sess := newFakeSession()
	h.auth.sess = sess
	newConn := newFakeConn()
	newConn.setTools([]RemoteTool{{Name: "get_issue"}})
	h.dialer.script("gh", fakeDial{conn: newConn})

	if err := h.m.Authenticate(context.Background(), "gh"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	got := statusOf(h.m, "gh")
	if got.State != core.MCPReady {
		t.Fatalf("State = %q, want ready", got.State)
	}
	for _, c := range h.rec.changesFor("gh") {
		if c.State == core.MCPFailed {
			t.Errorf("spurious failed event published: %+v (full sequence: %+v)", c, h.rec.changesFor("gh"))
		}
	}
	if !oldConn.isClosed() {
		t.Errorf("old conn was never closed")
	}
}

// TestManager_ServersDoesNotHoldLockAcrossHas proves Servers() releases
// m.mu before calling Tokens.Has, so a slow token store never blocks
// Tools() (called by the Runner every model step).
func TestManager_ServersDoesNotHoldLockAcrossHas(t *testing.T) {
	h := newHarness(t, httpServer("gh", "https://gh.example/mcp"))

	block := make(chan struct{})
	h.tokens.mu.Lock()
	h.tokens.block = block
	h.tokens.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.m.Servers()
		close(done)
	}()

	// Give Servers a moment to reach the blocked Has call.
	select {
	case <-done:
		t.Fatal("Servers() returned before Has was unblocked")
	case <-time.After(50 * time.Millisecond):
	}

	toolsDone := make(chan struct{})
	go func() {
		h.m.Tools()
		close(toolsDone)
	}()
	select {
	case <-toolsDone:
	case <-time.After(testTimeout):
		t.Fatal("Tools() blocked while Servers() was waiting on Has")
	}

	close(block)
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Servers() never returned after Has unblocked")
	}
}

// failure captured against an old (conn, gen) pair must not clobber a
// newer connection that Reconnect, Logout, or Authenticate installed in
// the meantime.
func TestTool_MarkFailedIgnoresStaleGeneration(t *testing.T) {
	h := newHarness(t, httpServer("srv", "https://srv.example/mcp"))
	oldConn := newFakeConn()
	h.dialer.script("srv", fakeDial{conn: oldConn})
	h.m.Start(context.Background())
	h.waitState("srv", core.MCPReady)

	conn, gen, ok := h.m.liveConn("srv")
	if !ok || conn != Conn(oldConn) {
		t.Fatalf("liveConn = %v, %v, %v, want oldConn, _, true", conn, gen, ok)
	}

	// A concurrent Reconnect installs a new conn/generation before the
	// stale call's failure is reported. waitState searches the whole
	// event history, and "ready" already appears once (from Start), so
	// wait for the *second* ready event instead of reusing the first.
	newConn := newFakeConn()
	h.dialer.script("srv", fakeDial{conn: newConn})
	if err := h.m.Reconnect(context.Background(), "srv"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	ok = h.rec.waitFor(t, testTimeout, func(evs []event.Event) bool {
		count := 0
		for _, e := range evs {
			if c, ok := e.(event.MCPServerChanged); ok && c.Name == "srv" && c.State == core.MCPReady {
				count++
			}
		}
		return count >= 2
	})
	if !ok {
		t.Fatalf("server never reconnected to ready a second time (events: %+v)", h.rec.changesFor("srv"))
	}

	before := len(h.rec.all())
	h.m.markFailed("srv", gen, errors.New("stale transport error"))

	got := statusOf(h.m, "srv")
	if got.State != core.MCPReady {
		t.Errorf("State = %q, want ready (stale markFailed must be ignored)", got.State)
	}
	if after := len(h.rec.all()); after != before {
		t.Errorf("stale markFailed published %d extra events, want 0", after-before)
	}
}
