package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// callbackPage is the fixed page shown after a callback; it echoes nothing
// from the request.
const callbackPage = "Signed in to jig. You can close this tab."

// callbackResult is what one /callback request delivered.
type callbackResult struct {
	code, state, iss string
	err              error
}

// Listener accepts exactly one OAuth redirect on 127.0.0.1, then shuts
// itself down.
type Listener struct {
	ln   net.Listener
	srv  *http.Server
	port int

	results chan callbackResult

	closeOnce sync.Once
	done      chan struct{}
}

// Listen binds 127.0.0.1:port. If that fails (e.g. the port is taken), it
// falls back to 127.0.0.1:0 (any free port).
func Listen(port int) (*Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("mcpauth: listen: %w", err)
		}
	}

	l := &Listener{
		ln:      ln,
		results: make(chan callbackResult, 1),
		done:    make(chan struct{}),
	}
	l.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", l.handleCallback)
	l.srv = &http.Server{Handler: mux}

	go func() {
		_ = l.srv.Serve(ln)
		close(l.done)
	}()

	return l, nil
}

// Port returns the bound port.
func (l *Listener) Port() int {
	return l.port
}

// RedirectURL returns the redirect URL this listener answers on.
func (l *Listener) RedirectURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", l.port)
}

// handleCallback answers only GET /callback; anything else is 404. The
// first request delivers its code/state/iss (or its error) to Wait, writes
// the fixed page, and shuts the server down in the background so this
// handler can finish writing its response first.
func (l *Listener) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/callback" {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	res := callbackResult{
		code:  q.Get("code"),
		state: q.Get("state"),
		iss:   q.Get("iss"),
	}
	if errCode := q.Get("error"); errCode != "" {
		res.err = fmt.Errorf("mcpauth: authorization error: %s: %s", errCode, q.Get("error_description"))
	}

	select {
	case l.results <- res:
	default:
		// A second callback after the first has already been delivered;
		// the server is shutting down anyway.
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(callbackPage))

	go l.Close()
}

// Wait blocks until the callback arrives, ctx is done, or Close is called
// first (in which case it returns ctx.Err() if ctx is already done, or an
// error otherwise). The result channel is buffered and checked before
// l.done, so a callback that already arrived (and triggered the
// self-shutdown) is always returned, even if the server has since finished
// closing.
func (l *Listener) Wait(ctx context.Context) (code, state, iss string, err error) {
	select {
	case res := <-l.results:
		return resultOrErr(res)
	default:
	}

	select {
	case res := <-l.results:
		return resultOrErr(res)
	case <-ctx.Done():
		return "", "", "", ctx.Err()
	case <-l.done:
		return "", "", "", errors.New("mcpauth: listener closed before a callback arrived")
	}
}

// resultOrErr splits a callbackResult into Wait's return values.
func resultOrErr(res callbackResult) (code, state, iss string, err error) {
	if res.err != nil {
		return "", "", "", res.err
	}
	return res.code, res.state, res.iss, nil
}

// Close shuts the server down. It is idempotent and safe to call more than
// once, including from the handler that just answered the one callback it
// waits for.
func (l *Listener) Close() {
	l.closeOnce.Do(func() {
		_ = l.srv.Close()
	})
}
