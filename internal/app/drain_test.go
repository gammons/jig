package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// gatedWriter blocks every Write until release is closed.
type gatedWriter struct {
	release chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	<-g.release
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Write(p)
}

func (g *gatedWriter) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

func TestRendering_FinishDrainsEveryPublishedEvent(t *testing.T) {
	const n = 50
	bus := event.NewBus()
	errw := &gatedWriter{release: make(chan struct{})}
	r := startRendering(bus, &bytes.Buffer{}, errw)

	// A second subscriber observes when finish publishes its marker.
	watch := bus.Subscribe()
	defer watch.Close()

	for range n {
		bus.Publish(event.ToolCallStarted{
			Base: event.Base{SessionID: "root"},
			Call: core.ToolCall{Name: "glob", Input: json.RawMessage(`{}`)},
		})
	}
	bus.Publish(event.RunFailed{Base: event.Base{SessionID: "root"}, Err: "boom"})

	finished := make(chan map[core.SessionID]bool, 1)
	go func() { finished <- r.finish() }()

	// Release the renderer only once the drain marker is on the bus, so
	// any shutdown that closes the subscription without waiting for the
	// marker would discard the still-queued events.
	waitForMarker(t, watch.C())
	close(errw.release)

	var reported map[core.SessionID]bool
	select {
	case reported = <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("finish did not return")
	}
	if got := strings.Count(errw.String(), "→ glob"); got != n {
		t.Errorf("rendered %d tool lines, want %d", got, n)
	}
	if !strings.HasSuffix(errw.String(), "error: boom\n") {
		t.Errorf("stderr does not end with the RunFailed line: %q", errw.String())
	}
	if !reported["root"] {
		t.Errorf("reported = %v, want root", reported)
	}
}

func waitForMarker(t *testing.T, c <-chan event.Event) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-c:
			if _, ok := e.(drained); ok {
				return
			}
		case <-timeout:
			t.Fatal("drain marker never published")
		}
	}
}
