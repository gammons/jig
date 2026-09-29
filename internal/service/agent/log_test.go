package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/core/logtest"
)

// signalLLM reports each Stream call on called before delegating to next.
type signalLLM struct {
	next   core.LLM
	called chan struct{}
}

func (s signalLLM) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	s.called <- struct{}{}
	return s.next.Stream(ctx, req)
}

func wantContains(t *testing.T, line string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(line, s) {
			t.Errorf("line %q lacks %q", line, s)
		}
	}
}

func only(t *testing.T, buf *logtest.Buffer, msg string) string {
	t.Helper()
	lines := buf.Find(msg)
	if len(lines) != 1 {
		t.Fatalf("%q lines = %d, want 1: %q", msg, len(lines), buf.Lines())
	}
	return lines[0]
}

func TestRunnerLog_TwoStepRun(t *testing.T) {
	llm := llmtest.New(llmtest.Calls(llmtest.Call("c1", "echo", `{}`)), llmtest.Text("done"))
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantContains(t, only(t, buf, "run start"), "cat=run", "session=ses_test", "root=ses_test", "depth=0", "agent=build", "model=prov/mod", "max_steps=100")
	if n := len(buf.Find("step start")); n != 2 {
		t.Errorf("step start lines = %d, want 2", n)
	}
	ends := buf.Find("step end")
	if len(ends) != 2 {
		t.Fatalf("step end lines = %d, want 2", len(ends))
	}
	wantContains(t, ends[0], "cat=step", "step=1", "calls=1", "status=complete")
	wantContains(t, ends[1], "step=2", "calls=0")
	wantContains(t, only(t, buf, "run end"), "outcome=done", "steps=2", "in=20", "out=10")
}

func TestRunnerLog_MaxSteps(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c3", "echo", `{}`)),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	f.rc.Agent.MaxSteps = 2
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantContains(t, only(t, buf, "run end"), "outcome=max_steps", "steps=2")
}

func TestRunnerLog_RetryThenSuccess(t *testing.T) {
	llm := llmtest.New(
		llmtest.Turn{Err: &core.LLMError{Retryable: true, Err: errors.New("overloaded")}},
		llmtest.Text("recovered"),
	)
	f := newFixture(t, llm)
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hi")
	f.clk.BlockUntilWaiters(1)
	f.clk.Advance(time.Second)
	if res := wait(t, done); res.err != nil {
		t.Fatalf("Run: %v", res.err)
	}

	wantContains(t, only(t, buf, "attempt failed"), "cat=retry", "attempt=1", "received=false", "retryable=true", "delay=1s", "err=overloaded")
	wantContains(t, only(t, buf, "run end"), "outcome=done")
}

func TestRunnerLog_FailureAfterFirstEvent(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{
		Events: []core.StreamEvent{{Kind: core.StreamText, Text: "partial"}},
		Err:    &core.LLMError{Retryable: true, Err: errors.New("stream reset")},
	})
	f := newFixture(t, llm)
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err == nil {
		t.Fatal("Run succeeded, want failure")
	}

	wantContains(t, only(t, buf, "attempt failed"), "received=true", "giving_up=true")
	wantContains(t, only(t, buf, "run end"), "outcome=failed", `err="stream reset"`)
}

func TestRunnerLog_Cancelled(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Hang: true})
	f := newFixture(t, llm)
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan runResult, 1)
	go func() {
		m, err := r.Run(ctx, f.rc, "hang")
		done <- runResult{m, err}
	}()
	waitStarted(t, f.rec)
	cancel()

	if res := wait(t, done); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	wantContains(t, only(t, buf, "run end"), "outcome=cancelled")
}

func TestRunnerLog_SetupFailure(t *testing.T) {
	f := newFixture(t, nil)
	f.deps.LLMs = fakeSource{err: errors.New("no such model")}
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if n := len(buf.Find("run start")); n != 0 {
		t.Errorf("run start lines = %d, want 0", n)
	}
	wantContains(t, only(t, buf, "run end"), "outcome=failed", "steps=0")
}

func TestRunnerLog_StepTiming(t *testing.T) {
	gate := make(chan struct{})
	inner := llmtest.New(llmtest.Turn{Gate: gate, Events: llmtest.Text("hi").Events})
	sig := signalLLM{next: inner, called: make(chan struct{}, 1)}
	f := newFixture(t, sig)
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hi")
	select {
	case <-sig.called:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for Stream")
	}
	f.clk.Advance(3 * time.Second)
	close(gate)

	if res := wait(t, done); res.err != nil {
		t.Fatalf("Run: %v", res.err)
	}
	wantContains(t, only(t, buf, "step end"), "first_event=3s", "stream=3s")
}
