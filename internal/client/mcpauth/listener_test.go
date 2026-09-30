package mcpauth

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestListener_Callback(t *testing.T) {
	l, err := Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := l.RedirectURL() + "?code=c&state=s&iss=i"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if got := string(body[:n]); got != callbackPage {
		t.Errorf("body = %q, want %q", got, callbackPage)
	}

	code, state, iss, err := l.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "c" || state != "s" || iss != "i" {
		t.Errorf("Wait() = %q,%q,%q, want c,s,i", code, state, iss)
	}

	// The server has shut down after answering the one callback.
	if _, err := http.Get(l.RedirectURL()); err == nil {
		t.Error("second request to a shut-down listener succeeded, want an error")
	}
}

func TestListener_RejectsOtherPaths(t *testing.T) {
	l, err := Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(l.Port()) + "/other")
	if err != nil {
		t.Fatalf("GET /other: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, _, err := l.Wait(ctx); err == nil {
		t.Error("Wait() succeeded after only a non-callback request, want a timeout error")
	}
}

func TestListener_BusyPortFallsBack(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("blocker listen: %v", err)
	}
	defer blocker.Close()
	busyPort := blocker.Addr().(*net.TCPAddr).Port

	l, err := Listen(busyPort)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	if l.Port() == busyPort {
		t.Errorf("Port() = %d, want a fallback port different from the busy one", l.Port())
	}
}

func TestListener_LoopbackOnly(t *testing.T) {
	l, err := Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	if l.ln.Addr().(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Errorf("bound IP = %s, want 127.0.0.1", l.ln.Addr().(*net.TCPAddr).IP.String())
	}
}
