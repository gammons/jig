package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/permission"
)

// stubTool is a configurable tool: run supplies its behavior.
type stubTool struct {
	name       string
	concurrent bool
	run        func(ctx context.Context, call core.ToolCall) (core.ToolResult, error)
}

func (s stubTool) Name() string           { return s.name }
func (s stubTool) Description() string    { return "stub" }
func (s stubTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (s stubTool) Concurrent() bool       { return s.concurrent }
func (s stubTool) Run(ctx context.Context, _ ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if s.run == nil {
		return core.ToolResult{Output: "ok:" + string(call.Input)}, nil
	}
	return s.run(ctx, call)
}

// stubHook records its invocations into log and applies the configured
// behavior.
type stubHook struct {
	name   string
	log    *callLog
	block  string
	err    error
	suffix string
}

func (h stubHook) Before(_ context.Context, _ ext.RunContext, _ ext.Tool, call core.ToolCall) (core.ToolCall, ext.Verdict, error) {
	h.log.add("before:" + h.name + ":" + call.ID)
	if h.err != nil {
		return call, ext.Verdict{}, h.err
	}
	if h.block != "" {
		return call, ext.Verdict{Block: true, Reason: h.block}, nil
	}
	return call, ext.Verdict{}, nil
}

func (h stubHook) After(_ context.Context, _ ext.RunContext, _ ext.Tool, call core.ToolCall, res core.ToolResult) core.ToolResult {
	h.log.add("after:" + h.name + ":" + call.ID)
	res.Output += h.suffix
	return res
}

type callLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.entries = append(l.entries, s)
	l.mu.Unlock()
}

func (l *callLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// newExecRunner returns a Runner whose only live deps are a frozen
// registry holding hooks and a recorder bus.
func newExecRunner(hooks ...ext.ToolHook) (*Runner, *recorder) {
	reg := ext.NewRegistry()
	for _, h := range hooks {
		if err := reg.AddToolHook(h); err != nil {
			panic(err)
		}
	}
	rec := newRecorder()
	return NewRunner(Deps{Ext: reg.Freeze(), Bus: rec}), rec
}

func execRC() ext.RunContext {
	return ext.RunContext{SessionID: "ses_x", MessageID: "msg_x", Agent: core.Agent{Name: "build"}}
}

func TestExec_ConcurrentBatchRunsInParallel(t *testing.T) {
	var mu sync.Mutex
	arrived := 0
	all := make(chan struct{})
	barrier := func(ctx context.Context, call core.ToolCall) (core.ToolResult, error) {
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return core.ToolResult{Output: "met"}, nil
		case <-time.After(waitTimeout):
			return core.ToolResult{Output: "barrier timeout: ran serially", IsError: true}, nil
		}
	}
	a := stubTool{name: "a", concurrent: true, run: barrier}
	b := stubTool{name: "b", concurrent: true, run: barrier}
	r, _ := newExecRunner()

	res := r.execute(context.Background(), execRC(), []ext.Tool{a, b},
		[]core.ToolCall{llmtest.Call("c1", "a", `{}`), llmtest.Call("c2", "b", `{}`)})
	if len(res) != 2 {
		t.Fatalf("results = %+v, want 2", res)
	}
	for _, x := range res {
		if x.IsError || x.Output != "met" {
			t.Errorf("result = %+v, want met", x)
		}
	}
}

func TestExec_NonConcurrentSerialAndOrdered(t *testing.T) {
	log := &callLog{}
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	run := func(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		log.add("run:" + call.ID)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return core.ToolResult{Output: call.ID}, nil
	}
	s := stubTool{name: "s", run: run}
	r, _ := newExecRunner()

	calls := []core.ToolCall{llmtest.Call("c1", "s", `{}`), llmtest.Call("c2", "s", `{}`), llmtest.Call("c3", "s", `{}`)}
	for range 20 {
		log.entries = nil
		res := r.execute(context.Background(), execRC(), []ext.Tool{s}, calls)
		if got := strings.Join(log.all(), ","); got != "run:c1,run:c2,run:c3" {
			t.Fatalf("run order = %s", got)
		}
		if len(res) != 3 {
			t.Fatalf("results = %+v", res)
		}
	}
	if maxInFlight != 1 {
		t.Errorf("max in flight = %d, want 1", maxInFlight)
	}
}

func TestExec_SerialCallWaitsForPrecedingBatch(t *testing.T) {
	log := &callLog{}
	gate := make(chan struct{})
	slow := stubTool{name: "slow", concurrent: true, run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		<-gate
		log.add("slow")
		return core.ToolResult{}, nil
	}}
	opener := stubTool{name: "opener", concurrent: true, run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		close(gate)
		return core.ToolResult{}, nil
	}}
	serial := stubTool{name: "serial", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		log.add("serial")
		return core.ToolResult{}, nil
	}}
	r, _ := newExecRunner()
	r.execute(context.Background(), execRC(), []ext.Tool{slow, opener, serial}, []core.ToolCall{
		llmtest.Call("c1", "slow", `{}`), llmtest.Call("c2", "opener", `{}`), llmtest.Call("c3", "serial", `{}`),
	})
	if got := strings.Join(log.all(), ","); got != "slow,serial" {
		t.Errorf("order = %s, want slow,serial", got)
	}
}

func TestExec_ResultsInCallOrder(t *testing.T) {
	gate := make(chan struct{})
	slow := stubTool{name: "slow", concurrent: true, run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		<-gate
		return core.ToolResult{Output: "slow"}, nil
	}}
	fast := stubTool{name: "fast", concurrent: true, run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		close(gate)
		return core.ToolResult{Output: "fast"}, nil
	}}
	echo := echoTool{name: "echo"}
	r, rec := newExecRunner()

	res := r.execute(context.Background(), execRC(), []ext.Tool{echo, fast, slow}, []core.ToolCall{
		llmtest.Call("c1", "slow", `{}`), llmtest.Call("c2", "fast", `{}`), llmtest.Call("c3", "echo", `{"k":1}`),
	})
	want := []struct{ id, name, out string }{{"c1", "slow", "slow"}, {"c2", "fast", "fast"}, {"c3", "echo", `echo:{"k":1}`}}
	if len(res) != len(want) {
		t.Fatalf("results = %+v", res)
	}
	for i, w := range want {
		if res[i].CallID != w.id || res[i].Name != w.name || res[i].Output != w.out {
			t.Errorf("result %d = %+v, want %+v", i, res[i], w)
		}
	}

	started, finished := 0, 0
	for _, e := range rec.all() {
		switch ev := e.(type) {
		case event.ToolCallStarted:
			started++
			if ev.Session() != "ses_x" || ev.MessageID != "msg_x" || ev.Call.ID == "" {
				t.Errorf("started = %+v", ev)
			}
		case event.ToolCallFinished:
			finished++
			if ev.Session() != "ses_x" || ev.MessageID != "msg_x" || ev.Result.CallID == "" {
				t.Errorf("finished = %+v", ev)
			}
		}
	}
	if started != 3 || finished != 3 {
		t.Errorf("started=%d finished=%d, want 3 each", started, finished)
	}
}

func TestRunner_UnknownToolBecomesErrorResult(t *testing.T) {
	llm := llmtest.New(llmtest.Calls(llmtest.Call("c1", "nope", `{}`)), llmtest.Text("done"))
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}, echoTool{name: "alpha"}))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	res := f.messages()[1].Parts[1].Result
	want := `tool "nope" is not available to agent "build"; available: alpha, echo`
	if res == nil || !res.IsError || res.Output != want || res.CallID != "c1" || res.Name != "nope" {
		t.Errorf("result = %+v, want %q", res, want)
	}
}

func TestRunner_MalformedInputBecomesErrorResult(t *testing.T) {
	for _, input := range []string{`not json`, `[1,2]`, `"str"`} {
		t.Run(input, func(t *testing.T) {
			ran := false
			tool := stubTool{name: "x", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
				ran = true
				return core.ToolResult{}, nil
			}}
			llm := llmtest.New(llmtest.Calls(llmtest.Call("c1", "x", input)), llmtest.Text("done"))
			f := newFixture(t, llm, withTools(tool))
			r := NewRunner(f.deps)

			if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			res := f.messages()[1].Parts[1].Result
			if res == nil || !res.IsError || !strings.HasPrefix(res.Output, "invalid JSON input for x: ") || res.CallID != "c1" {
				t.Errorf("result = %+v, want invalid JSON error", res)
			}
			if ran {
				t.Error("tool ran on malformed input")
			}
		})
	}
}

func TestExec_EmptyInputIsEmptyObject(t *testing.T) {
	r, _ := newExecRunner()
	res := r.execute(context.Background(), execRC(), []ext.Tool{stubTool{name: "x"}},
		[]core.ToolCall{{ID: "c1", Name: "x"}})
	if res[0].IsError || res[0].Output != "ok:{}" {
		t.Errorf("result = %+v, want ok:{}", res[0])
	}
}

func TestExec_HookBlocks(t *testing.T) {
	log := &callLog{}
	ran := false
	tool := stubTool{name: "x", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		ran = true
		return core.ToolResult{}, nil
	}}
	r, _ := newExecRunner(stubHook{name: "h1", log: log}, stubHook{name: "h2", log: log, block: "nope, not today"})

	res := r.execute(context.Background(), execRC(), []ext.Tool{tool}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
	if !res[0].IsError || res[0].Output != "nope, not today" || res[0].CallID != "c1" || res[0].Name != "x" {
		t.Errorf("result = %+v, want blocked", res[0])
	}
	if ran {
		t.Error("blocked tool ran")
	}
	if got := strings.Join(log.all(), ","); got != "before:h1:c1,before:h2:c1" {
		t.Errorf("hook log = %s", got)
	}
}

func TestExec_HooksRunInOrderAndAfterRewrites(t *testing.T) {
	log := &callLog{}
	r, _ := newExecRunner(stubHook{name: "h1", log: log, suffix: "+1"}, stubHook{name: "h2", log: log, suffix: "+2"})
	res := r.execute(context.Background(), execRC(), []ext.Tool{stubTool{name: "x"}}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
	if res[0].Output != "ok:{}+1+2" || res[0].IsError {
		t.Errorf("result = %+v", res[0])
	}
	if got := strings.Join(log.all(), ","); got != "before:h1:c1,before:h2:c1,after:h1:c1,after:h2:c1" {
		t.Errorf("hook log = %s", got)
	}
}

func TestExec_HookErrorBecomesErrorResult(t *testing.T) {
	r, _ := newExecRunner(stubHook{name: "h", log: &callLog{}, err: errors.New("hook broke")})
	res := r.execute(context.Background(), execRC(), []ext.Tool{stubTool{name: "x"}}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
	if !res[0].IsError || res[0].Output != "hook broke" {
		t.Errorf("result = %+v, want hook error", res[0])
	}
}

func TestExec_ToolErrorBecomesErrorResult(t *testing.T) {
	tool := stubTool{name: "x", run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		return core.ToolResult{}, errors.New("disk on fire")
	}}
	r, _ := newExecRunner()
	res := r.execute(context.Background(), execRC(), []ext.Tool{tool}, []core.ToolCall{llmtest.Call("c1", "x", `{}`)})
	if !res[0].IsError || res[0].Output != "disk on fire" || res[0].CallID != "c1" {
		t.Errorf("result = %+v", res[0])
	}
}

func TestExec_PanicRecovered(t *testing.T) {
	boom := stubTool{name: "boom", concurrent: true, run: func(context.Context, core.ToolCall) (core.ToolResult, error) {
		panic("kaboom")
	}}
	r, _ := newExecRunner()
	res := r.execute(context.Background(), execRC(), []ext.Tool{boom, stubTool{name: "x", concurrent: true}},
		[]core.ToolCall{llmtest.Call("c1", "boom", `{}`), llmtest.Call("c2", "x", `{}`)})
	if !res[0].IsError || res[0].Output != "tool boom panicked: kaboom" || res[0].CallID != "c1" {
		t.Errorf("result 0 = %+v, want panic error", res[0])
	}
	if res[1].IsError || res[1].Output != "ok:{}" {
		t.Errorf("result 1 = %+v, want ok", res[1])
	}
}

func TestExec_CancelledBeforeStartIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := newExecRunner()
	res := r.execute(ctx, execRC(), []ext.Tool{stubTool{name: "x"}},
		[]core.ToolCall{llmtest.Call("c1", "x", `{}`), llmtest.Call("c2", "x", `{}`)})
	for i, x := range res {
		if !x.IsError || x.Output != "cancelled" || x.CallID == "" {
			t.Errorf("result %d = %+v, want cancelled", i, x)
		}
	}
}

// permFixture wires a Runner around a real event.Bus, a BusAsker, and a
// permission.Hook asking for every call to the concurrent "ask" tool.
type permFixture struct {
	*fixture
	bus   *event.Bus
	asker *permission.BusAsker
	sub   *event.Subscription
}

func newPermFixture(t *testing.T, llm core.LLM, tools ...ext.Tool) *permFixture {
	t.Helper()
	bus := event.NewBus()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	asker := permission.NewBusAsker(bus, ids.New(clk, rand.Reader))
	hook := permission.NewHook(core.PermissionRules{"ask": {Default: core.Ask}}, asker)
	ask := stubTool{name: "ask", concurrent: true}
	f := newFixture(t, llm, withTools(append(tools, ask)...), func(r *ext.Registry) {
		if err := r.AddToolHook(hook); err != nil {
			panic(err)
		}
	})
	f.deps.Bus = bus
	sub := bus.Subscribe()
	t.Cleanup(sub.Close)
	return &permFixture{fixture: f, bus: bus, asker: asker, sub: sub}
}

func (p *permFixture) waitPermissionRequests(n int) {
	p.t.Helper()
	timeout := time.After(waitTimeout)
	for n > 0 {
		select {
		case e := <-p.sub.C():
			if _, ok := e.(event.PermissionRequested); ok {
				n--
			}
		case <-timeout:
			p.t.Fatalf("timed out waiting for %d more PermissionRequested", n)
		}
	}
}

func TestRunner_CancelDuringPermissionPrompt(t *testing.T) {
	llm := llmtest.New(llmtest.Calls(llmtest.Call("c1", "ask", `{}`), llmtest.Call("c2", "ask", `{}`)))
	p := newPermFixture(t, llm)
	r := NewRunner(p.deps)

	done := p.runAsync(r, "go")
	p.waitPermissionRequests(2)
	r.Cancel(p.rc.SessionID)

	if res := wait(t, done); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	asst := p.messages()[1]
	if asst.Status != core.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", asst.Status)
	}
	var results []*core.ToolResult
	for _, part := range asst.Parts {
		if part.Kind == core.PartToolResult {
			results = append(results, part.Result)
		}
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	for i, res := range results {
		if res == nil || !res.IsError || res.Output != "cancelled" {
			t.Errorf("result %d = %+v, want cancelled", i, res)
		}
	}
	if n := p.asker.Pending(); n != 0 {
		t.Errorf("asker.Pending() = %d, want 0", n)
	}
}

func TestRunner_CancelledRunPairsEveryToolCall(t *testing.T) {
	llm := llmtest.New(llmtest.Calls(
		llmtest.Call("c1", "ask", `{}`), llmtest.Call("c2", "ask", `{}`),
		llmtest.Call("c3", "echo", `{}`), llmtest.Call("c4", "ask", `{}`),
	))
	p := newPermFixture(t, llm, echoTool{name: "echo"})
	r := NewRunner(p.deps)

	done := p.runAsync(r, "go")
	p.waitPermissionRequests(2)
	r.Cancel(p.rc.SessionID)
	if res := wait(t, done); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}

	calls, results := 0, map[string]int{}
	for _, m := range p.messages() {
		for _, part := range m.Parts {
			switch {
			case part.Kind == core.PartToolCall && part.Call != nil:
				calls++
			case part.Kind == core.PartToolResult && part.Result != nil:
				results[part.Result.CallID]++
			}
		}
	}
	for _, m := range p.messages() {
		for _, part := range m.Parts {
			if part.Kind == core.PartToolCall && part.Call != nil && results[part.Call.ID] != 1 {
				t.Errorf("call %s has %d results, want 1", part.Call.ID, results[part.Call.ID])
			}
		}
	}
	if calls != 4 || len(results) != 4 {
		t.Errorf("calls=%d results=%v, want 4 paired", calls, results)
	}
}

// panicHook panics in Before for calls with ID id.
type panicHook struct{ id string }

func (h panicHook) Before(_ context.Context, _ ext.RunContext, _ ext.Tool, call core.ToolCall) (core.ToolCall, ext.Verdict, error) {
	if call.ID == h.id {
		panic("hook kaboom")
	}
	return call, ext.Verdict{}, nil
}

func (h panicHook) After(_ context.Context, _ ext.RunContext, _ ext.Tool, _ core.ToolCall, res core.ToolResult) core.ToolResult {
	return res
}

func TestExec_HookPanicInConcurrentBatchRecovered(t *testing.T) {
	r, rec := newExecRunner(panicHook{id: "c1"})
	x := stubTool{name: "x", concurrent: true}
	res := r.execute(context.Background(), execRC(), []ext.Tool{x},
		[]core.ToolCall{llmtest.Call("c1", "x", `{}`), llmtest.Call("c2", "x", `{}`)})
	if !res[0].IsError || res[0].Output != "tool x panicked: hook kaboom" || res[0].CallID != "c1" || res[0].Name != "x" {
		t.Errorf("result 0 = %+v, want hook panic error", res[0])
	}
	if res[1].IsError || res[1].Output != "ok:{}" || res[1].CallID != "c2" {
		t.Errorf("result 1 = %+v, want ok", res[1])
	}
	finished := map[string]bool{}
	for _, e := range rec.all() {
		if f, ok := e.(event.ToolCallFinished); ok {
			finished[f.Result.CallID] = true
		}
	}
	if !finished["c1"] || !finished["c2"] {
		t.Errorf("finished = %v, want c1 and c2", finished)
	}
}
