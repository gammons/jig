package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/logtest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func logClock() clock.Clock {
	return clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func newLoggedClient(log *slog.Logger) *http.Client {
	return &http.Client{Transport: NewLoggingTransport(nil, log, logClock())}
}

func doReq(t *testing.T, c *http.Client, method, url string, attrs ...slog.Attr) *http.Response {
	t.Helper()
	ctx := core.WithLogAttrs(context.Background(), attrs...)
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func wantContains(t *testing.T, line string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(line, s) {
			t.Errorf("line %q missing %q", line, s)
		}
	}
}

func wantNotContains(t *testing.T, line string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(line, s) {
			t.Errorf("line %q unexpectedly contains %q", line, s)
		}
	}
}

func TestLoggingTransport_OKStream(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("request-id", "req_1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("def"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()
	log, buf := logtest.New()

	resp := doReq(t, newLoggedClient(log), http.MethodPost, srv.URL+"/v1/messages?key=secret", slog.String("session", "ses_c"))
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != "abcdef" {
		t.Fatalf("body = %q, %v", b, err)
	}
	_ = resp.Body.Close()

	reqs := buf.Find("http request")
	if len(reqs) != 1 {
		t.Fatalf("http request lines = %v", reqs)
	}
	wantContains(t, reqs[0], "cat=http", "method=POST", "host=127.0.0.1:", "path=/v1/messages", "session=ses_c")
	wantNotContains(t, reqs[0], "secret", "key=")
	rs := buf.Find("http response")
	if len(rs) != 1 {
		t.Fatalf("http response lines = %v", rs)
	}
	wantContains(t, rs[0], "status=200", "req_id=req_1", "ttfb=", "session=ses_c")
	wantNotContains(t, rs[0], "body=")
	done := buf.Find("http body done")
	if len(done) != 1 {
		t.Fatalf("http body done lines = %v", done)
	}
	wantContains(t, done[0], "bytes=6", "dur=")
	wantNotContains(t, done[0], "read_err")
}

func TestLoggingTransport_ErrorBodyCapped(t *testing.T) {
	t.Parallel()
	payload := strings.Repeat("x", 10*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req_2")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()
	log, buf := logtest.New()

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer sk-secret")
	resp, err := newLoggedClient(log).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(b) != payload {
		t.Fatalf("client read %d bytes, err %v; want %d unchanged", len(b), err, len(payload))
	}

	rs := buf.Find("http response")
	if len(rs) != 1 {
		t.Fatalf("http response lines = %v", rs)
	}
	wantContains(t, rs[0], "status=502", "req_id=req_2", "body="+strings.Repeat("x", 4096))
	wantNotContains(t, rs[0], strings.Repeat("x", 4097))
	for _, l := range buf.Lines() {
		wantNotContains(t, l, "sk-secret", "Authorization")
	}
	done := buf.Find("http body done")
	if len(done) != 1 {
		t.Fatalf("http body done lines = %v", done)
	}
	wantContains(t, done[0], "bytes=10240")
}

func TestLoggingTransport_EmptyErrorBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	log, buf := logtest.New()

	resp := doReq(t, newLoggedClient(log), http.MethodPost, srv.URL+"/x")
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || len(b) != 0 {
		t.Fatalf("body = %q, %v", b, err)
	}
	rs := buf.Find("http response")
	if len(rs) != 1 {
		t.Fatalf("http response lines = %v", rs)
	}
	wantContains(t, rs[0], "status=502", `body=""`)
}

func TestLoggingTransport_TruncatedStream(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("0123456789"))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()
	log, buf := logtest.New()

	resp := doReq(t, newLoggedClient(log), http.MethodPost, srv.URL+"/x")
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err == nil {
		t.Fatalf("want read error, got nil after %q", b)
	}
	if string(b) != "0123456789" {
		t.Fatalf("body = %q", b)
	}
	done := buf.Find("http body done")
	if len(done) != 1 {
		t.Fatalf("http body done lines = %v", done)
	}
	wantContains(t, done[0], "bytes=10", "read_err=")
}

func TestLoggingTransport_TransportError(t *testing.T) {
	t.Parallel()
	log, buf := logtest.New()
	dialErr := errors.New("dial refused")
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, dialErr })
	rt := NewLoggingTransport(base, log, logClock())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.invalid/x", nil)
	resp, err := rt.RoundTrip(req)
	if resp != nil || !errors.Is(err, dialErr) || err != dialErr {
		t.Fatalf("resp, err = %v, %v; want nil, %v", resp, err, dialErr)
	}
	es := buf.Find("http error")
	if len(es) != 1 {
		t.Fatalf("http error lines = %v", es)
	}
	wantContains(t, es[0], `err="dial refused"`, "dur=", "cat=http")
	if len(buf.Find("http response")) != 0 {
		t.Errorf("unexpected http response line")
	}
}

func TestLoggingTransport_BodyDoneLoggedOnce(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(srv.Close)

	t.Run("read to EOF then close", func(t *testing.T) {
		t.Parallel()
		log, buf := logtest.New()
		resp := doReq(t, newLoggedClient(log), http.MethodGet, srv.URL+"/x")
		if _, err := io.ReadAll(resp.Body); err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		_ = resp.Body.Close()
		done := buf.Find("http body done")
		if len(done) != 1 {
			t.Fatalf("http body done lines = %v", done)
		}
		wantContains(t, done[0], "bytes=5")
	})

	t.Run("close without reading", func(t *testing.T) {
		t.Parallel()
		log, buf := logtest.New()
		resp := doReq(t, newLoggedClient(log), http.MethodGet, srv.URL+"/x")
		_ = resp.Body.Close()
		done := buf.Find("http body done")
		if len(done) != 1 {
			t.Fatalf("http body done lines = %v", done)
		}
		wantContains(t, done[0], "bytes=0")
	})
}

func TestLoggingTransport_ConcurrentRequestsKeepTheirAttrs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	log, buf := logtest.New()
	c := newLoggedClient(log)

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := doReq(t, c, http.MethodPost, fmt.Sprintf("%s/req/%d", srv.URL, i), slog.String("session", fmt.Sprintf("ses_%d", i)))
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()

	for i := range n {
		tag := fmt.Sprintf("session=ses_%d", i)
		path := fmt.Sprintf("path=/req/%d", i)
		reqLines := 0
		for _, l := range buf.Lines() {
			if !strings.Contains(l, tag) {
				continue
			}
			if strings.Contains(l, `msg="http request"`) {
				reqLines++
				wantContains(t, l, path)
			}
			for j := range n {
				if j != i {
					wantNotContains(t, l, fmt.Sprintf("session=ses_%d", j), fmt.Sprintf("path=/req/%d", j))
				}
			}
		}
		if reqLines != 1 {
			t.Errorf("session ses_%d: %d http request lines, want 1", i, reqLines)
		}
	}
}
