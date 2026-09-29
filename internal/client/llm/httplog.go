package llm

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
)

// errBodyPrefix is how much of a non-2xx response body is logged.
const errBodyPrefix = 4096

// NewLoggingTransport wraps base (http.DefaultTransport when nil) so each
// request, response, and response-body lifecycle is logged to log at debug
// level. It never alters the bytes or errors the caller sees, and never logs
// headers (other than a request-id value) or a request body.
func NewLoggingTransport(base http.RoundTripper, log *slog.Logger, clk clock.Clock) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &loggingTransport{base: base, log: log, clk: clk}
}

type loggingTransport struct {
	base http.RoundTripper
	log  *slog.Logger
	clk  clock.Clock
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	common := append([]any{"cat", "http"}, core.LogArgs(req.Context())...)
	t.log.Debug("http request", slices.Concat(common,
		[]any{"method", req.Method, "host", req.URL.Host, "path", req.URL.Path})...)

	start := t.clk.Now()
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.log.Debug("http error", slices.Concat(common,
			[]any{"err", err, "dur", t.clk.Now().Sub(start)})...)
		return resp, err
	}

	args := slices.Concat(common, []any{"status", resp.StatusCode, "ttfb", t.clk.Now().Sub(start)})
	if id := requestID(resp.Header); id != "" {
		args = append(args, "req_id", id)
	}
	if resp.Body == nil || resp.StatusCode == http.StatusSwitchingProtocols {
		t.log.Debug("http response", args...)
		return resp, nil
	}

	orig := resp.Body
	var r io.Reader = orig
	if resp.StatusCode >= 300 {
		prefix := make([]byte, errBodyPrefix)
		// A short or empty body ends with EOF/ErrUnexpectedEOF; any other
		// error is left for the caller to hit again on its own read.
		n, _ := io.ReadFull(io.LimitReader(orig, errBodyPrefix), prefix)
		prefix = prefix[:n]
		args = append(args, "body", string(prefix))
		r = io.MultiReader(bytes.NewReader(prefix), orig)
	}
	t.log.Debug("http response", args...)

	resp.Body = &loggedBody{
		r: r, closer: orig, log: t.log, clk: t.clk, start: start, common: common,
	}
	return resp, nil
}

func requestID(h http.Header) string {
	for _, k := range []string{"request-id", "x-request-id", "cf-ray"} {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// loggedBody counts the bytes the caller reads and logs "http body done"
// exactly once: on EOF, the first other read error, or Close.
type loggedBody struct {
	r      io.Reader
	closer io.Closer
	log    *slog.Logger
	clk    clock.Clock
	start  time.Time
	common []any

	n    atomic.Int64
	once sync.Once
}

func (b *loggedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n.Add(int64(n))
	if err == io.EOF { //nolint:errorlint // only the bare sentinel is a clean end
		b.done(nil)
	} else if err != nil {
		b.done(err)
	}
	return n, err
}

func (b *loggedBody) Close() error {
	err := b.closer.Close()
	b.done(nil)
	return err
}

func (b *loggedBody) done(readErr error) {
	b.once.Do(func() {
		args := slices.Concat(b.common, []any{"bytes", b.n.Load(), "dur", b.clk.Now().Sub(b.start)})
		if readErr != nil {
			args = append(args, "read_err", readErr)
		}
		b.log.Debug("http body done", args...)
	})
}
