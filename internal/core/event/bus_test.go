package event

import (
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

// recv reads one event from sub, failing the test if none arrives within a
// generous bound. This is a failure-guard timeout, not a timing assertion.
func recv(t *testing.T, sub *Subscription) Event {
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

func TestBus_DeliversInOrder(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe()
	defer sub.Close()

	sid := core.SessionID("s1")
	mid := core.MessageID("m1")

	b.Publish(MessageStarted{Base: Base{SessionID: sid}, MessageID: mid, Agent: "coder", Model: "x"})
	b.Publish(ToolCallStarted{Base: Base{SessionID: sid}, MessageID: mid, Call: core.ToolCall{ID: "c1", Name: "bash"}})
	b.Publish(RunFinished{Base: Base{SessionID: sid}, MessageID: mid, Usage: core.Usage{Input: 1}, CostUSD: 0.01})

	first := recv(t, sub)
	if _, ok := first.(MessageStarted); !ok {
		t.Fatalf("first event: got %T, want MessageStarted", first)
	}

	second := recv(t, sub)
	if _, ok := second.(ToolCallStarted); !ok {
		t.Fatalf("second event: got %T, want ToolCallStarted", second)
	}

	third := recv(t, sub)
	if _, ok := third.(RunFinished); !ok {
		t.Fatalf("third event: got %T, want RunFinished", third)
	}
}

func TestBus_MergesAdjacentDeltasWhenSubscriberLags(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe()
	defer sub.Close()

	sid := core.SessionID("s1")
	mid := core.MessageID("m1")

	for i := 0; i < 100; i++ {
		b.Publish(TextDelta{Base: Base{SessionID: sid}, MessageID: mid, Text: "a"})
	}
	b.Publish(RunFinished{Base: Base{SessionID: sid}, MessageID: mid})

	var text string
	var count int
	for {
		e := recv(t, sub)
		count++
		if td, ok := e.(TextDelta); ok {
			text += td.Text
			continue
		}
		if _, ok := e.(RunFinished); ok {
			break
		}
		t.Fatalf("unexpected event type %T", e)
	}

	if want := 100; len(text) != want {
		t.Fatalf("got %d concatenated chars, want %d", len(text), want)
	}
	if count >= 100 {
		t.Fatalf("got %d events, want < 100 (merging should have reduced the count)", count)
	}
}

func TestBus_DoesNotMergeAcrossMessagesOrKinds(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe()
	defer sub.Close()

	sid := core.SessionID("s1")
	m1 := core.MessageID("m1")
	m2 := core.MessageID("m2")

	// Publish all before reading, so they sit in the pending queue together.
	b.Publish(TextDelta{Base: Base{SessionID: sid}, MessageID: m1, Text: "x"})
	b.Publish(TextDelta{Base: Base{SessionID: sid}, MessageID: m2, Text: "y"})
	b.Publish(ReasoningDelta{Base: Base{SessionID: sid}, MessageID: m2, Text: "r"})
	b.Publish(TextDelta{Base: Base{SessionID: sid}, MessageID: m2, Text: "z"})

	var got []Event
	for i := 0; i < 4; i++ {
		got = append(got, recv(t, sub))
	}

	if len(got) != 4 {
		t.Fatalf("got %d events, want 4", len(got))
	}
}

func TestBus_NeverMergesLifecycle(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe()
	defer sub.Close()

	sid := core.SessionID("s1")

	for i := 0; i < 50; i++ {
		b.Publish(ToolCallFinished{Base: Base{SessionID: sid}, Result: core.ToolResult{CallID: "c", Output: "ok"}})
	}

	var count int
	for i := 0; i < 50; i++ {
		e := recv(t, sub)
		if _, ok := e.(ToolCallFinished); !ok {
			t.Fatalf("event %d: got %T, want ToolCallFinished", i, e)
		}
		count++
	}
	if count != 50 {
		t.Fatalf("got %d events, want 50", count)
	}
}

func TestBus_CloseStopsDelivery(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe()

	sub.Close()

	select {
	case _, ok := <-sub.C():
		if ok {
			t.Fatal("expected closed channel, got an event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel to close")
	}

	// Publish after Close must not panic or block.
	done := make(chan struct{})
	go func() {
		b.Publish(RunFailed{Base: Base{SessionID: core.SessionID("s1")}, Err: "boom"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish after Close blocked")
	}

	// Closing again must not panic.
	sub.Close()
}

func TestBus_SubscribersIndependent(t *testing.T) {
	b := NewBus()
	lagging := b.Subscribe()
	defer lagging.Close()
	active := b.Subscribe()
	defer active.Close()

	sid := core.SessionID("s1")

	// The lagging subscriber never reads. Publishing must not block, and
	// the active subscriber must still receive events.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(SessionCreated{Base: Base{SessionID: sid}, Info: core.Session{ID: sid}})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked because a lagging subscriber never reads")
	}

	for i := 0; i < 10; i++ {
		e := recv(t, active)
		if _, ok := e.(SessionCreated); !ok {
			t.Fatalf("event %d: got %T, want SessionCreated", i, e)
		}
	}
}
