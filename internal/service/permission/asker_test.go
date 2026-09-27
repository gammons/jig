package permission

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/ids"
)

func newGen() *ids.Gen {
	return ids.New(clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), rand.Reader)
}

// recvEvent reads one event from sub with a failure-guard timeout.
func recvEvent(t *testing.T, sub *event.Subscription) event.Event {
	t.Helper()
	select {
	case e, ok := <-sub.C():
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func TestBusAsker_PublishesRequestAndResolved(t *testing.T) {
	bus := event.NewBus()
	sub := bus.Subscribe()
	defer sub.Close()

	b := NewBusAsker(bus, newGen())

	req := Request{
		SessionID: core.SessionID("s1"),
		Tool:      "bash",
		Subject:   "git status",
		Call:      core.ToolCall{ID: "c1", Name: "bash"},
	}

	type result struct {
		reply core.PermissionReply
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		reply, err := b.Ask(context.Background(), req)
		resCh <- result{reply, err}
	}()

	e := recvEvent(t, sub)
	pr, ok := e.(event.PermissionRequested)
	if !ok {
		t.Fatalf("event type = %T, want PermissionRequested", e)
	}
	if pr.Session() != req.SessionID {
		t.Errorf("PermissionRequested.Session() = %q, want %q", pr.Session(), req.SessionID)
	}
	if pr.Tool != "bash" || pr.Subject != "git status" {
		t.Errorf("PermissionRequested = %+v, want Tool=bash Subject=%q", pr, "git status")
	}
	if !strings.HasPrefix(pr.RequestID, "perm_") {
		t.Errorf("RequestID = %q, want prefix perm_", pr.RequestID)
	}
	if b.Pending() != 1 {
		t.Errorf("Pending() = %d, want 1", b.Pending())
	}

	want := core.PermissionReply{Kind: core.ReplyOnce}
	if err := b.Reply(pr.RequestID, want); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	select {
	case r := <-resCh:
		if r.err != nil {
			t.Fatalf("Ask err = %v, want nil", r.err)
		}
		if r.reply != want {
			t.Errorf("Ask reply = %+v, want %+v", r.reply, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Ask to return")
	}

	resolved := recvEvent(t, sub)
	rr, ok := resolved.(event.PermissionResolved)
	if !ok {
		t.Fatalf("event type = %T, want PermissionResolved", resolved)
	}
	if rr.RequestID != pr.RequestID {
		t.Errorf("PermissionResolved.RequestID = %q, want %q", rr.RequestID, pr.RequestID)
	}
	if rr.Reply != want {
		t.Errorf("PermissionResolved.Reply = %+v, want %+v", rr.Reply, want)
	}

	if b.Pending() != 0 {
		t.Errorf("Pending() after Reply = %d, want 0", b.Pending())
	}
}

func TestBusAsker_ReplyUnknownIDErrors(t *testing.T) {
	bus := event.NewBus()
	b := NewBusAsker(bus, newGen())

	err := b.Reply("perm_bogus", core.PermissionReply{Kind: core.ReplyOnce})
	if err == nil {
		t.Fatal("Reply: want error for unknown id, got nil")
	}
	want := `permission: unknown request "perm_bogus"`
	if err.Error() != want {
		t.Errorf("Reply err = %q, want %q", err.Error(), want)
	}
}

func TestBusAsker_ReplyInvalidKindErrors(t *testing.T) {
	bus := event.NewBus()
	sub := bus.Subscribe()
	defer sub.Close()

	b := NewBusAsker(bus, newGen())

	resCh := make(chan error, 1)
	go func() {
		_, err := b.Ask(context.Background(), Request{SessionID: core.SessionID("s1"), Tool: "bash"})
		resCh <- err
	}()

	e := recvEvent(t, sub)
	pr := e.(event.PermissionRequested)

	err := b.Reply(pr.RequestID, core.PermissionReply{Kind: "bogus"})
	if err == nil {
		t.Fatal("Reply: want error for invalid kind, got nil")
	}
	want := `permission: invalid reply kind "bogus"`
	if err.Error() != want {
		t.Errorf("Reply err = %q, want %q", err.Error(), want)
	}

	// The request must still be pending: an invalid Kind must not resolve
	// it, so Ask is still blocked and Pending() still counts it.
	if b.Pending() != 1 {
		t.Errorf("Pending() after invalid Reply = %d, want 1 (request must stay pending)", b.Pending())
	}
	select {
	case err := <-resCh:
		t.Fatalf("Ask returned early (err=%v), want it still blocked", err)
	case <-time.After(50 * time.Millisecond):
	}

	// A valid Reply for the same id must still work.
	if err := b.Reply(pr.RequestID, core.PermissionReply{Kind: core.ReplyOnce}); err != nil {
		t.Fatalf("Reply with valid kind: %v", err)
	}
	select {
	case err := <-resCh:
		if err != nil {
			t.Fatalf("Ask err = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Ask to return after valid Reply")
	}
}

func TestAsk_ContextCancelUnblocks(t *testing.T) {
	bus := event.NewBus()
	sub := bus.Subscribe()
	defer sub.Close()

	b := NewBusAsker(bus, newGen())
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		reply core.PermissionReply
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		reply, err := b.Ask(ctx, Request{SessionID: core.SessionID("s1"), Tool: "bash"})
		resCh <- result{reply, err}
	}()

	e := recvEvent(t, sub)
	pr := e.(event.PermissionRequested)

	cancel()

	select {
	case r := <-resCh:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Ask err = %v, want context.Canceled", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Ask to unblock on ctx cancel")
	}

	// The pending entry must have been removed: Pending() drops, and a
	// later Reply for that id errors instead of blocking forever.
	if b.Pending() != 0 {
		t.Errorf("Pending() after cancel = %d, want 0", b.Pending())
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Reply(pr.RequestID, core.PermissionReply{Kind: core.ReplyOnce})
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Reply after cancel: want error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reply after cancel blocked instead of erroring")
	}
}

func TestStaticAsker(t *testing.T) {
	t.Run("Allow true replies once", func(t *testing.T) {
		a := StaticAsker{Allow: true}
		reply, err := a.Ask(context.Background(), Request{})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if reply.Kind != core.ReplyOnce {
			t.Errorf("reply.Kind = %q, want %q", reply.Kind, core.ReplyOnce)
		}
	})

	t.Run("Allow false replies deny with message", func(t *testing.T) {
		a := StaticAsker{Allow: false}
		reply, err := a.Ask(context.Background(), Request{})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if reply.Kind != core.ReplyDeny {
			t.Errorf("reply.Kind = %q, want %q", reply.Kind, core.ReplyDeny)
		}
		want := "permission required (re-run with --yes)"
		if reply.Message != want {
			t.Errorf("reply.Message = %q, want %q", reply.Message, want)
		}
	})
}

func TestAsk_ContextCancelPublishesResolved(t *testing.T) {
	bus := event.NewBus()
	sub := bus.Subscribe()
	defer sub.Close()

	b := NewBusAsker(bus, newGen())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = b.Ask(ctx, Request{SessionID: core.SessionID("s1"), Tool: "bash"})
	}()

	pr := recvEvent(t, sub).(event.PermissionRequested)
	cancel()
	<-done

	rr, ok := recvEvent(t, sub).(event.PermissionResolved)
	if !ok {
		t.Fatal("want PermissionResolved after cancellation")
	}
	want := core.PermissionReply{Kind: core.ReplyDeny, Message: "cancelled"}
	if rr.RequestID != pr.RequestID || rr.Reply != want || rr.Session() != "s1" {
		t.Errorf("PermissionResolved = %+v, want %s %+v on s1", rr, pr.RequestID, want)
	}
}
