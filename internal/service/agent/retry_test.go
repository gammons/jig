package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestRunner_RetriesBeforeFirstEvent(t *testing.T) {
	llm := llmtest.New(
		llmtest.Turn{Err: &core.LLMError{Retryable: true, Err: errors.New("overloaded")}},
		llmtest.Text("recovered"),
	)
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hi")
	f.clk.BlockUntilWaiters(1)
	f.clk.Advance(time.Second)

	res := wait(t, done)
	if res.err != nil {
		t.Fatalf("Run: %v", res.err)
	}
	if textOf(res.msg) != "recovered" || res.msg.Status != core.StatusComplete {
		t.Errorf("message = %+v", res.msg)
	}
	if n := len(llm.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	started := 0
	for _, e := range f.rec.all() {
		if _, ok := e.(event.MessageStarted); ok {
			started++
		}
	}
	if started != 1 {
		t.Errorf("MessageStarted published %d times, want once per step", started)
	}
}

func TestRunner_RetryGivesUpAfterThreeAttempts(t *testing.T) {
	retryable := llmtest.Turn{Err: &core.LLMError{Retryable: true, Err: errors.New("overloaded")}}
	llm := llmtest.New(retryable, retryable, retryable, llmtest.Text("never"))
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hi")
	f.clk.BlockUntilWaiters(1)
	f.clk.Advance(time.Second)
	f.clk.BlockUntilWaiters(1)
	f.clk.Advance(2 * time.Second)

	res := wait(t, done)
	var le *core.LLMError
	if !errors.As(res.err, &le) {
		t.Fatalf("err = %v, want the LLMError", res.err)
	}
	if n := len(llm.Requests()); n != 3 {
		t.Errorf("requests = %d, want 3", n)
	}
	if msgs := f.messages(); msgs[1].Status != core.StatusFailed {
		t.Errorf("status = %s, want failed", msgs[1].Status)
	}
}

func TestRunner_NoRetryAfterPartialOutput(t *testing.T) {
	llm := llmtest.New(
		llmtest.Turn{
			Events: []core.StreamEvent{{Kind: core.StreamText, Text: "partial"}},
			Err:    &core.LLMError{Retryable: true, Err: errors.New("overloaded")},
		},
		llmtest.Text("never"),
	)
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	_, err := r.Run(context.Background(), f.rc, "hi")
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if n := len(llm.Requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
	msgs := f.messages()
	if len(msgs) != 2 || msgs[1].Status != core.StatusFailed || textOf(msgs[1]) != "partial" {
		t.Errorf("messages = %+v, want failed assistant keeping partial text", msgs)
	}
	evs := f.rec.all()
	if rf, ok := evs[len(evs)-1].(event.RunFailed); !ok || rf.Err != "overloaded" {
		t.Errorf("last event = %#v, want RunFailed{overloaded}", evs[len(evs)-1])
	}
}

func TestRunner_NonRetryableFails(t *testing.T) {
	cases := map[string]error{
		"plain":         errors.New("bad request"),
		"not retryable": &core.LLMError{Retryable: false, Err: errors.New("bad request")},
	}
	for name, streamErr := range cases {
		t.Run(name, func(t *testing.T) {
			llm := llmtest.New(llmtest.Turn{Err: streamErr}, llmtest.Text("never"))
			f := newFixture(t, llm)
			r := NewRunner(f.deps)

			_, err := r.Run(context.Background(), f.rc, "hi")
			if !errors.Is(err, streamErr) {
				t.Fatalf("err = %v, want %v", err, streamErr)
			}
			if n := len(llm.Requests()); n != 1 {
				t.Errorf("requests = %d, want 1", n)
			}
			if msgs := f.messages(); len(msgs) != 2 || msgs[1].Status != core.StatusFailed {
				t.Errorf("messages = %+v, want failed assistant", msgs)
			}
			evs := f.rec.all()
			if rf, ok := evs[len(evs)-1].(event.RunFailed); !ok || rf.Err != "bad request" {
				t.Errorf("last event = %#v, want RunFailed{bad request}", evs[len(evs)-1])
			}
		})
	}
}

func TestRunner_CancelDuringRetryWait(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Err: &core.LLMError{Retryable: true, Err: errors.New("overloaded")}})
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hi")
	f.clk.BlockUntilWaiters(1)
	r.Cancel(f.rc.SessionID)

	if res := wait(t, done); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	if msgs := f.messages(); msgs[1].Status != core.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", msgs[1].Status)
	}
}

func TestRetryDelay(t *testing.T) {
	retryable := &core.LLMError{Retryable: true}
	tests := []struct {
		name     string
		err      error
		received bool
		attempt  int
		want     time.Duration
		ok       bool
	}{
		{"first retry", retryable, false, 1, time.Second, true},
		{"second retry", retryable, false, 2, 2 * time.Second, true},
		{"attempts exhausted", retryable, false, 3, 0, false},
		{"retry-after wins", &core.LLMError{Retryable: true, RetryAfter: 7 * time.Second}, false, 1, 7 * time.Second, true},
		{"after an event", retryable, true, 1, 0, false},
		{"not retryable", &core.LLMError{}, false, 1, 0, false},
		{"plain error", errors.New("x"), false, 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := retryDelay(tt.err, tt.received, tt.attempt)
			if got != tt.want || ok != tt.ok {
				t.Errorf("retryDelay = %v, %v; want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
