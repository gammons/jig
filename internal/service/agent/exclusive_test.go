package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestRun_BusyDuringCompact(t *testing.T) {
	llm := llmtest.New(llmtest.Text("hi"), llmtest.Text("hi again"))
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	block := make(chan struct{})
	release := make(chan struct{})
	excDone := make(chan error, 1)
	go func() {
		excDone <- r.Exclusive(context.Background(), f.rc.SessionID, func(context.Context) error {
			close(block)
			<-release
			return nil
		})
	}()
	<-block

	if _, err := r.Run(context.Background(), f.rc, "one"); !errors.Is(err, core.ErrBusy) {
		t.Fatalf("Run err = %v, want ErrBusy", err)
	}

	close(release)
	if err := <-excDone; err != nil {
		t.Fatalf("Exclusive: %v", err)
	}

	if _, err := r.Run(context.Background(), f.rc, "two"); err != nil {
		t.Fatalf("Run after Exclusive released: %v", err)
	}
}

func TestExclusive_CancelStopsFn(t *testing.T) {
	f := newFixture(t, llmtest.New())
	r := NewRunner(f.deps)

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.Exclusive(context.Background(), f.rc.SessionID, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-started
	r.Cancel(f.rc.SessionID)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Exclusive err = %v, want context.Canceled", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for Exclusive")
	}
}
