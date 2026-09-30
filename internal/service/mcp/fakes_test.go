package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// fakeDial scripts one call to fakeDialer.Dial for one server: either a
// ready conn, an error, or a block until ctx is done.
type fakeDial struct {
	conn    *fakeConn
	err     error
	block   bool
	started chan struct{} // closed once Dial has been entered, for tests that need to know the goroutine is running
	release chan struct{} // if set, Dial waits for this (or ctx.Done()) before resolving conn/err
}

// fakeDialer is a scriptable Dialer: each server name gets a queue of
// fakeDial results, consumed in order across repeated Dial calls (so
// Reconnect/Authenticate tests can script a second attempt).
type fakeDialer struct {
	mu    sync.Mutex
	calls []dialCall
	next  map[string][]fakeDial
}

type dialCall struct {
	name string
	auth AuthMode
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{next: make(map[string][]fakeDial)}
}

// script queues one Dial result for name.
func (d *fakeDialer) script(name string, r fakeDial) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next[name] = append(d.next[name], r)
}

func (d *fakeDialer) Dial(ctx context.Context, srv core.MCPServer, auth AuthMode, onChanged func()) (Conn, error) {
	d.mu.Lock()
	d.calls = append(d.calls, dialCall{name: srv.Name, auth: auth})
	queue := d.next[srv.Name]
	var r fakeDial
	if len(queue) > 0 {
		r = queue[0]
		d.next[srv.Name] = queue[1:]
	}
	d.mu.Unlock()

	if r.block {
		if r.started != nil {
			close(r.started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.release != nil {
		if r.started != nil {
			close(r.started)
		}
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.conn != nil {
		r.conn.onChanged = onChanged
		return r.conn, nil
	}
	return newFakeConn(), nil
}

func (d *fakeDialer) authModes(name string) []AuthMode {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []AuthMode
	for _, c := range d.calls {
		if c.name == name {
			out = append(out, c.auth)
		}
	}
	return out
}

// fakeConn is a scriptable Conn.
type fakeConn struct {
	mu sync.Mutex

	tools     []RemoteTool
	listErr   error
	callErr   error
	callRes   RemoteResult
	closed    bool
	closeErr  error
	done      chan struct{}
	err       error
	onChanged func()

	// callBlock, if set, makes CallTool block until it's closed or ctx is
	// done, for timeout/cancel tests.
	callBlock chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{done: make(chan struct{})}
}

func (c *fakeConn) ListTools(ctx context.Context) ([]RemoteTool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]RemoteTool(nil), c.tools...), nil
}

func (c *fakeConn) CallTool(ctx context.Context, name string, args json.RawMessage) (RemoteResult, error) {
	c.mu.Lock()
	block := c.callBlock
	c.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return RemoteResult{}, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.callErr != nil {
		return RemoteResult{}, c.callErr
	}
	return c.callRes, nil
}

// dropDoneOnly closes Done without marking closed or setting Err, so a
// subsequent CallTool error is treated as a transport failure by tool.go's
// Done-check, matching a real conn's behavior.
func (c *fakeConn) dropDoneOnly() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	return c.closeErr
}

func (c *fakeConn) Done() <-chan struct{} { return c.done }

func (c *fakeConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// dropWithErr simulates the connection ending on its own (not via Close):
// it sets Err and closes Done, without marking closed (so a second Close
// call from the Manager, if any, doesn't panic).
func (c *fakeConn) dropWithErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}

func (c *fakeConn) setTools(tools []RemoteTool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools = tools
}

func (c *fakeConn) fireChanged() {
	c.mu.Lock()
	cb := c.onChanged
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// fakeSession is a scriptable Authenticator Session.
type fakeSession struct {
	urlCh  chan string
	closed chan struct{}
}

func newFakeSession() *fakeSession {
	return &fakeSession{urlCh: make(chan string, 1), closed: make(chan struct{})}
}

func (s *fakeSession) URL() <-chan string { return s.urlCh }
func (s *fakeSession) Close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}

// fakeAuth is a scriptable Authenticator.
type fakeAuth struct {
	mu    sync.Mutex
	sess  *fakeSession
	err   error
	calls int
}

func (a *fakeAuth) Begin(ctx context.Context, srv core.MCPServer) (Session, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if a.err != nil {
		return nil, a.err
	}
	return a.sess, nil
}

// fakeTokens is a scriptable Tokens.
type fakeTokens struct {
	mu      sync.Mutex
	has     map[string]bool
	deleted []string
	delErr  error

	// block, if non-nil, is read once by Has before returning, so a test
	// can hold Has open while asserting other Manager calls still work.
	block chan struct{}
}

func newFakeTokens() *fakeTokens {
	return &fakeTokens{has: make(map[string]bool)}
}

func (t *fakeTokens) Has(url string) bool {
	t.mu.Lock()
	block := t.block
	t.mu.Unlock()
	if block != nil {
		<-block
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.has[url]
}

func (t *fakeTokens) Delete(url string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deleted = append(t.deleted, url)
	return t.delErr
}

func (t *fakeTokens) deletedURLs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.deleted...)
}

// recorder is a Publisher that records every event, in order, and lets
// tests wait for a predicate over the events seen so far without polling.
type recorder struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []event.Event
}

func newRecorder() *recorder {
	r := &recorder{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *recorder) Publish(e event.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *recorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

func (r *recorder) changesFor(name string) []event.MCPServerChanged {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []event.MCPServerChanged
	for _, e := range r.events {
		if c, ok := e.(event.MCPServerChanged); ok && c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// waitFor blocks until pred(all events so far) is true, or timeout
// elapses (a real wall-clock bound on the test itself, via time.After —
// never time.Now/time.Sleep, which the repo's hygiene check forbids in
// test files).
func (r *recorder) waitFor(t testingT, timeout time.Duration, pred func([]event.Event) bool) bool {
	t.Helper()
	done := make(chan struct{})
	timedOut := make(chan struct{})
	go func() {
		select {
		case <-time.After(timeout):
			r.mu.Lock()
			close(timedOut)
			r.mu.Unlock()
			r.cond.Broadcast()
		case <-done:
		}
	}()
	defer close(done)

	r.mu.Lock()
	defer r.mu.Unlock()
	for !pred(r.events) {
		select {
		case <-timedOut:
			return false
		default:
		}
		r.cond.Wait()
	}
	return true
}

// testingT is the subset of *testing.T that fakes_test.go needs, so this
// file doesn't have to import "testing" just for a Helper() call.
type testingT interface {
	Helper()
}
