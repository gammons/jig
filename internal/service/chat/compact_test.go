package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestCompact_BusyDuringRun(t *testing.T) {
	gate := make(chan struct{})
	main := llmtest.New(llmtest.Text("first"), llmtest.Turn{
		Gate: gate,
		Events: []core.StreamEvent{
			{Kind: core.StreamText, Text: "second"},
			{Kind: core.StreamFinish, FinishReason: "stop"},
		},
	})
	f := newFixture(t, main, llmtest.New(llmtest.Text("Title")))

	first, err := f.svc.Send(context.Background(), core.SendRequest{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	drainStarted(f.rec)

	secondDone := make(chan error, 1)
	go func() {
		_, err := f.svc.Send(context.Background(), core.SendRequest{SessionID: first.SessionID, Text: "again"})
		secondDone <- err
	}()
	waitStarted(t, f.rec)

	if err := f.svc.Compact(context.Background(), first.SessionID); !errors.Is(err, core.ErrBusy) {
		t.Fatalf("Compact err = %v, want core.ErrBusy", err)
	}

	close(gate)
	if err := <-secondDone; err != nil {
		t.Fatalf("second Send: %v", err)
	}
}

func TestCompact_MissingSessionIsConfigError(t *testing.T) {
	f := newFixture(t, llmtest.New(), llmtest.New())
	err := f.svc.Compact(context.Background(), "ses_missing")
	wantConfigError(t, err)
}
