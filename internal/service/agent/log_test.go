package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
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

func lineFor(t *testing.T, lines []string, sub string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	t.Fatalf("no line contains %q: %q", sub, lines)
	return ""
}

func TestRunnerLog_ToolCall(t *testing.T) {
	big := stubTool{name: "big", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		return core.ToolResult{Output: strings.Repeat("x", 60*1024)}, nil
	}}
	llm := llmtest.New(llmtest.Calls(llmtest.Call("c1", "big", `{}`)), llmtest.Text("done"))
	f := newFixture(t, llm, withTools(big))
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	capped := len(capOutput(strings.Repeat("x", 60*1024)))
	wantContains(t, only(t, buf, "tool call"), "cat=tool", "tool=big", "call_id=c1", "out_bytes=61440",
		"out_bytes_capped="+strconv.Itoa(capped), "is_error=false", "blocked=false", "session=ses_test")
}

type blockHook struct{}

func (blockHook) Before(_ context.Context, _ ext.RunContext, tool ext.Tool, call core.ToolCall) (core.ToolCall, ext.Verdict, error) {
	return call, ext.Verdict{Block: tool.Name() == "echo", Reason: "denied"}, nil
}

func (blockHook) After(_ context.Context, _ ext.RunContext, _ ext.Tool, _ core.ToolCall, res core.ToolResult) core.ToolResult {
	return res
}

func TestRunnerLog_ToolBlockedAndUnknown(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`), llmtest.Call("c2", "nope", `{}`)),
		llmtest.Text("done"),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}), func(r *ext.Registry) {
		if err := r.AddToolHook(blockHook{}); err != nil {
			panic(err)
		}
	})
	var buf *logtest.Buffer
	f.deps.Log, buf = logtest.New()
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := buf.Find("tool call")
	if len(lines) != 2 {
		t.Fatalf("tool call lines = %d, want 2: %q", len(lines), lines)
	}
	c1 := lineFor(t, lines, "call_id=c1")
	wantContains(t, c1, "blocked=true", "is_error=true")
	if strings.Contains(c1, "reason=") {
		t.Errorf("blocked line %q has a reason", c1)
	}
	wantContains(t, lineFor(t, lines, "call_id=c2"), "is_error=true", "blocked=false", "reason=unknown")
}

func TestRunnerLog_ToolInvalidAndCancelled(t *testing.T) {
	r, _ := newExecRunner()
	var buf *logtest.Buffer
	r.d.Log, buf = logtest.New()

	tools := []ext.Tool{echoTool{name: "echo"}}
	r.execute(context.Background(), execRC(), tools, []core.ToolCall{llmtest.Call("c1", "echo", `[1]`)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.execute(ctx, execRC(), tools, []core.ToolCall{llmtest.Call("c2", "echo", `{}`)})

	lines := buf.Find("tool call")
	wantContains(t, lineFor(t, lines, "call_id=c1"), "reason=invalid", "is_error=true")
	wantContains(t, lineFor(t, lines, "call_id=c2"), "reason=cancelled", "is_error=true")
}

func TestRunnerLog_ToolPanicCountsAsRan(t *testing.T) {
	boom := stubTool{name: "boom", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		panic("kaboom")
	}}
	r, _ := newExecRunner()
	var buf *logtest.Buffer
	r.d.Log, buf = logtest.New()

	r.execute(context.Background(), execRC(), []ext.Tool{boom}, []core.ToolCall{llmtest.Call("c1", "boom", `{}`)})

	line := only(t, buf, "tool call")
	wantContains(t, line, "is_error=true", "blocked=false")
	if strings.Contains(line, "reason=") {
		t.Errorf("line %q has a reason", line)
	}
}

func TestRunnerLog_ToolDuration(t *testing.T) {
	r, _ := newExecRunner()
	clk := r.d.Clock.(*clock.Fake)
	var buf *logtest.Buffer
	r.d.Log, buf = logtest.New()
	slow := stubTool{name: "slow", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		clk.Advance(2 * time.Second)
		return core.ToolResult{Output: "ok"}, nil
	}}

	r.execute(context.Background(), execRC(), []ext.Tool{slow}, []core.ToolCall{llmtest.Call("c1", "slow", `{}`)})

	wantContains(t, only(t, buf, "tool call"), "dur=2s")
}

// attrsLLM records the log attrs of the ctx each Stream call receives.
type attrsLLM struct {
	next  core.LLM
	attrs *[]slog.Attr
}

func (a attrsLLM) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	*a.attrs = core.LogAttrs(ctx)
	return a.next.Stream(ctx, req)
}

func TestRunnerLog_StreamContextCarriesAttrs(t *testing.T) {
	var got []slog.Attr
	f := newFixture(t, nil)
	f.deps.LLMs = fakeSource{llm: attrsLLM{next: llmtest.New(llmtest.Text("hi")), attrs: &got}, info: testInfo()}
	f.rc.RootID = ""
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got0 := fmt.Sprint(got)
	wantContains(t, got0, "root=ses_test", "session=ses_test", "depth=0")
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
