package ui

import (
	"testing"

	"github.com/gammons/jig/internal/core/event"
)

func TestBridge_DeliversAndStopsOnClose(t *testing.T) {
	t.Parallel()
	bus := event.NewBus()
	sub := bus.Subscribe()

	want := event.RunFailed{Base: rootBase(), Err: "boom"}
	bus.Publish(want)

	msg, ok := waitEvent(sub)().(eventMsg)
	if !ok {
		t.Fatalf("waitEvent yielded %T, want eventMsg", msg)
	}
	if got, ok := msg.ev.(event.RunFailed); !ok || got != want {
		t.Fatalf("event = %#v, want %#v", msg.ev, want)
	}

	sub.Close()
	if msg := waitEvent(sub)(); msg != nil {
		t.Fatalf("after Close: waitEvent yielded %#v, want nil", msg)
	}
	if waitEvent(nil) != nil {
		t.Fatal("waitEvent(nil) should be a nil Cmd")
	}
}
