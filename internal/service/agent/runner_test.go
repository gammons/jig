package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/llmtest"
	"github.com/gammons/jig/internal/data/store"
	"github.com/gammons/jig/internal/ids"
	"github.com/gammons/jig/internal/service/agents"
)

// waitTimeout bounds how long a test waits for a background Run.
const waitTimeout = 5 * time.Second

type fakeSource struct {
	llm  core.LLM
	info core.ModelInfo
	err  error
}

func (s fakeSource) For(core.ModelRef) (core.LLM, core.ModelInfo, error) {
	return s.llm, s.info, s.err
}

type storeHistory struct{ s *store.Store }

func (h storeHistory) History(ctx context.Context, id core.SessionID) ([]core.Message, error) {
	return h.s.ListMessages(ctx, id)
}

// notifyStore forwards saves to a real store and reports each saved
// message on saved.
type notifyStore struct {
	s     *store.Store
	saved chan core.Message
}

func (n notifyStore) SaveMessage(ctx context.Context, m core.Message) error {
	err := n.s.SaveMessage(ctx, m)
	select {
	case n.saved <- m:
	default:
	}
	return err
}

type recorder struct {
	mu      sync.Mutex
	events  []event.Event
	started chan struct{}
}

func newRecorder() *recorder { return &recorder{started: make(chan struct{}, 16)} }

func (r *recorder) Publish(e event.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	if _, ok := e.(event.MessageStarted); ok {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}
}

func (r *recorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

type echoTool struct{ name string }

func (e echoTool) Name() string           { return e.name }
func (e echoTool) Description() string    { return "echoes its input" }
func (e echoTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (e echoTool) Concurrent() bool       { return false }
func (e echoTool) Run(_ context.Context, _ ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	return core.ToolResult{CallID: call.ID, Name: e.name, Output: "echo:" + string(call.Input)}, nil
}

type sysTransform struct{ text string }

func (s sysTransform) Priority() int { return 0 }
func (s sysTransform) Transform(_ context.Context, _ ext.RunContext, req *core.LLMRequest) error {
	req.System = append(req.System, s.text)
	return nil
}

type failTransform struct{}

func (failTransform) Priority() int { return 0 }
func (failTransform) Transform(context.Context, ext.RunContext, *core.LLMRequest) error {
	return errors.New("transform exploded")
}

type fixture struct {
	t     *testing.T
	st    *store.Store
	clk   *clock.Fake
	rec   *recorder
	deps  Deps
	rc    ext.RunContext
	model core.ModelRef
}

func testInfo() core.ModelInfo {
	return core.ModelInfo{DefaultMaxTokens: 4096, CostIn: 2, CostOut: 4}
}

// newFixture wires a Deps around a real store in t.TempDir(), a fake clock,
// and an event recorder. regs registers extensions before freezing.
func newFixture(t *testing.T, llm core.LLM, regs ...func(*ext.Registry)) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	sid := core.SessionID("ses_test")
	if err := st.CreateSession(ctx, core.Session{ID: sid, CreatedAt: clk.Now(), UpdatedAt: clk.Now()}); err != nil {
		t.Fatal(err)
	}

	reg := ext.NewRegistry()
	for _, fn := range regs {
		fn(reg)
	}
	rec := newRecorder()
	model := core.ModelRef{Provider: "prov", Model: "mod"}
	return &fixture{
		t:   t,
		st:  st,
		clk: clk,
		rec: rec,
		deps: Deps{
			LLMs:     fakeSource{llm: llm, info: testInfo()},
			Ext:      reg.Freeze(),
			Store:    st,
			History:  storeHistory{st},
			Bus:      rec,
			Clock:    clk,
			IDs:      ids.New(clk, rand.Reader),
			ToolsFor: agents.ToolsFor,
		},
		rc: ext.RunContext{
			SessionID: sid,
			RootID:    sid,
			Agent:     core.Agent{Name: "build"},
			Model:     model,
			WorkDir:   t.TempDir(),
		},
		model: model,
	}
}

func withTools(tools ...ext.Tool) func(*ext.Registry) {
	return func(r *ext.Registry) {
		for _, tool := range tools {
			if err := r.AddTool(tool); err != nil {
				panic(err)
			}
		}
	}
}

func withTransforms(ts ...ext.ContextTransform) func(*ext.Registry) {
	return func(r *ext.Registry) {
		for _, tr := range ts {
			if err := r.AddTransform(tr); err != nil {
				panic(err)
			}
		}
	}
}

func (f *fixture) messages() []core.Message {
	f.t.Helper()
	msgs, err := f.st.ListMessages(context.Background(), f.rc.SessionID)
	if err != nil {
		f.t.Fatal(err)
	}
	return msgs
}

type runResult struct {
	msg core.Message
	err error
}

func (f *fixture) runAsync(r *Runner, text string) <-chan runResult {
	out := make(chan runResult, 1)
	go func() {
		m, err := r.Run(context.Background(), f.rc, text)
		out <- runResult{m, err}
	}()
	return out
}

func wait(t *testing.T, ch <-chan runResult) runResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for Run")
		return runResult{}
	}
}

func waitStarted(t *testing.T, rec *recorder) {
	t.Helper()
	select {
	case <-rec.started:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for MessageStarted")
	}
}

func textOf(m core.Message) string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == core.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestRunner_TextOnlyTurn(t *testing.T) {
	llm := llmtest.New(llmtest.Text("hi there"))
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	got, err := r.Run(context.Background(), f.rc, "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Role != core.RoleAssistant || got.Status != core.StatusComplete || textOf(got) != "hi there" {
		t.Fatalf("returned message = %+v", got)
	}

	msgs := f.messages()
	if len(msgs) != 2 {
		t.Fatalf("stored %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != core.RoleUser || textOf(msgs[0]) != "hello" || msgs[0].Status != core.StatusComplete {
		t.Errorf("user message = %+v", msgs[0])
	}
	asst := msgs[1]
	if asst.Role != core.RoleAssistant || asst.Status != core.StatusComplete || textOf(asst) != "hi there" {
		t.Errorf("assistant message = %+v", asst)
	}
	if asst.Agent != "build" || asst.Model != "prov/mod" || asst.ID != got.ID {
		t.Errorf("assistant agent/model/id = %q %q %q", asst.Agent, asst.Model, asst.ID)
	}

	evs := f.rec.all()
	if len(evs) != 3 {
		t.Fatalf("events = %#v, want 3", evs)
	}
	if ms, ok := evs[0].(event.MessageStarted); !ok || ms.MessageID != got.ID || ms.Agent != "build" || ms.Model != "prov/mod" {
		t.Errorf("event 0 = %#v, want MessageStarted", evs[0])
	}
	if td, ok := evs[1].(event.TextDelta); !ok || td.Text != "hi there" || td.MessageID != got.ID {
		t.Errorf("event 1 = %#v, want TextDelta", evs[1])
	}
	if rf, ok := evs[2].(event.RunFinished); !ok || rf.MessageID != got.ID || rf.Session() != f.rc.SessionID {
		t.Errorf("event 2 = %#v, want RunFinished", evs[2])
	}
}

func TestRunner_RequestHasTransformsToolsAndHistory(t *testing.T) {
	llm := llmtest.New(llmtest.Text("ok"))
	f := newFixture(t, llm,
		withTools(echoTool{name: "echo"}, echoTool{name: "hidden"}),
		withTransforms(sysTransform{text: "sys-a"}, sysTransform{text: "sys-b"}),
	)
	f.rc.Agent.Tools = []string{"echo"}
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "question"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := llm.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Model != f.model {
		t.Errorf("Model = %v, want %v", req.Model, f.model)
	}
	if req.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %d, want 4096", req.MaxOutputTokens)
	}
	if strings.Join(req.System, ",") != "sys-a,sys-b" {
		t.Errorf("System = %q, want transforms applied in order", req.System)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "echo" || req.Tools[0].Description != "echoes its input" ||
		req.Tools[0].Schema["type"] != "object" {
		t.Errorf("Tools = %+v, want only echo", req.Tools)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != core.RoleUser || textOf(req.Messages[0]) != "question" {
		t.Errorf("Messages = %+v, want the stored user message", req.Messages)
	}
}

func TestRunner_CostFromModelInfo(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Text("done"),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// llmtest's default usage is {Input: 10, Output: 5} per step.
	perStep := 10*2/1e6 + 5*4/1e6
	msgs := f.messages()
	if len(msgs) != 3 {
		t.Fatalf("stored %d messages, want 3", len(msgs))
	}
	for _, m := range msgs[1:] {
		if m.Usage != (core.Usage{Input: 10, Output: 5}) || !approxEqual(m.CostUSD, perStep) {
			t.Errorf("message %s usage=%+v cost=%v, want per-step values", m.ID, m.Usage, m.CostUSD)
		}
	}
	evs := f.rec.all()
	rf, ok := evs[len(evs)-1].(event.RunFinished)
	if !ok {
		t.Fatalf("last event = %#v, want RunFinished", evs[len(evs)-1])
	}
	if rf.Usage != (core.Usage{Input: 20, Output: 10}) || !approxEqual(rf.CostUSD, 2*perStep) {
		t.Errorf("RunFinished usage=%+v cost=%v, want summed", rf.Usage, rf.CostUSD)
	}
}

func TestRunner_ToolResultsOnSameMessage(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{"x":1}`), llmtest.Call("c2", "nope", `{}`)),
		llmtest.Text("done"),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := f.messages()
	if len(msgs) != 3 {
		t.Fatalf("stored %d messages, want 3", len(msgs))
	}
	step1 := msgs[1]
	if step1.Status != core.StatusComplete || len(step1.Parts) != 4 {
		t.Fatalf("step 1 = %+v, want complete with 2 calls + 2 results", step1)
	}
	res1, res2 := step1.Parts[2].Result, step1.Parts[3].Result
	if res1 == nil || res1.CallID != "c1" || res1.Output != `echo:{"x":1}` || res1.IsError {
		t.Errorf("result 1 = %+v", res1)
	}
	if res2 == nil || res2.CallID != "c2" || !res2.IsError {
		t.Errorf("result 2 = %+v, want IsError for unknown tool", res2)
	}
	reqs := llm.Requests()
	if len(reqs) != 2 || len(reqs[1].Messages) != 2 || reqs[1].Messages[1].ID != step1.ID {
		t.Errorf("second request history = %+v, want user + step 1", reqs[len(reqs)-1].Messages)
	}
}

func TestRunner_MaxStepsStops(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c3", "echo", `{}`)),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	f.rc.Agent.MaxSteps = 2
	r := NewRunner(f.deps)

	got, err := r.Run(context.Background(), f.rc, "loop")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := len(llm.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	last := got.Parts[len(got.Parts)-1]
	if last.Kind != core.PartText || last.Text != "[stopped: reached max_steps (2)]" {
		t.Errorf("final part = %+v, want max_steps notice", last)
	}
	msgs := f.messages()
	stored := msgs[len(msgs)-1]
	if !strings.Contains(textOf(stored), "reached max_steps (2)") || stored.Status != core.StatusComplete {
		t.Errorf("stored final message = %+v", stored)
	}
}

func TestRunner_BusyRejected(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Hang: true})
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	first := f.runAsync(r, "one")
	waitStarted(t, f.rec)

	if _, err := r.Run(context.Background(), f.rc, "two"); !errors.Is(err, core.ErrBusy) {
		t.Fatalf("second Run err = %v, want ErrBusy", err)
	}
	r.Cancel(f.rc.SessionID)
	if res := wait(t, first); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("first Run err = %v, want context.Canceled", res.err)
	}
	// Only the first run's user message was saved.
	if msgs := f.messages(); len(msgs) != 2 {
		t.Errorf("stored %d messages, want 2", len(msgs))
	}
}

func TestRunner_CancelMarksInterrupted(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Hang: true})
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	done := f.runAsync(r, "hang")
	waitStarted(t, f.rec)
	r.Cancel(f.rc.SessionID)

	res := wait(t, done)
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	msgs := f.messages()
	if len(msgs) != 2 || msgs[1].Status != core.StatusInterrupted {
		t.Fatalf("messages = %+v, want assistant interrupted", msgs)
	}
	evs := f.rec.all()
	if rf, ok := evs[len(evs)-1].(event.RunFailed); !ok || rf.Err != "cancelled" {
		t.Errorf("last event = %#v, want RunFailed{cancelled}", evs[len(evs)-1])
	}
	// The session is free again.
	r.mu.Lock()
	n := len(r.running)
	r.mu.Unlock()
	if n != 0 {
		t.Errorf("running = %d, want 0 after Run returns", n)
	}
}

func TestRunner_CancelAnswersPendingToolCalls(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{
		Events: []core.StreamEvent{{Kind: core.StreamToolCall, Call: &core.ToolCall{ID: "c1", Name: "echo"}}},
		Hang:   true,
	})
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	ns := notifyStore{s: f.st, saved: make(chan core.Message, 8)}
	f.deps.Store = ns
	r := NewRunner(f.deps)

	done := f.runAsync(r, "go")
	waitForSave(t, ns.saved, func(m core.Message) bool {
		return m.Role == core.RoleAssistant && len(m.Parts) == 1 && m.Parts[0].Kind == core.PartToolCall
	})
	r.Cancel(f.rc.SessionID)

	if res := wait(t, done); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", res.err)
	}
	asst := f.messages()[1]
	if asst.Status != core.StatusInterrupted || len(asst.Parts) != 2 {
		t.Fatalf("assistant = %+v, want interrupted with call + result", asst)
	}
	res := asst.Parts[1].Result
	if res == nil || res.CallID != "c1" || res.Output != "cancelled" || !res.IsError {
		t.Errorf("result = %+v, want cancelled error", res)
	}
}

func waitForSave(t *testing.T, saved <-chan core.Message, match func(core.Message) bool) {
	t.Helper()
	timeout := time.After(waitTimeout)
	for {
		select {
		case m := <-saved:
			if match(m) {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for save")
		}
	}
}

func TestRunner_ParentCancelInterrupts(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Hang: true})
	f := newFixture(t, llm)
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
	if msgs := f.messages(); msgs[1].Status != core.StatusInterrupted {
		t.Errorf("status = %s, want interrupted", msgs[1].Status)
	}
}

func TestRunner_SourceErrorFails(t *testing.T) {
	f := newFixture(t, nil)
	f.deps.LLMs = fakeSource{err: errors.New("no such model")}
	r := NewRunner(f.deps)

	_, err := r.Run(context.Background(), f.rc, "hi")
	if err == nil || !strings.Contains(err.Error(), "no such model") {
		t.Fatalf("err = %v, want source error", err)
	}
	if msgs := f.messages(); len(msgs) != 1 || msgs[0].Role != core.RoleUser {
		t.Errorf("messages = %+v, want only the user message", msgs)
	}
	evs := f.rec.all()
	if len(evs) != 1 {
		t.Fatalf("events = %#v, want one RunFailed", evs)
	}
	if rf, ok := evs[0].(event.RunFailed); !ok || !strings.Contains(rf.Err, "no such model") {
		t.Errorf("event = %#v, want RunFailed", evs[0])
	}
}

func TestRunner_TransformErrorFails(t *testing.T) {
	llm := llmtest.New(llmtest.Text("never"))
	f := newFixture(t, llm, withTransforms(failTransform{}))
	r := NewRunner(f.deps)

	_, err := r.Run(context.Background(), f.rc, "hi")
	if err == nil || !strings.Contains(err.Error(), "transform exploded") {
		t.Fatalf("err = %v, want transform error", err)
	}
	if n := len(llm.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
	msgs := f.messages()
	if len(msgs) != 2 || msgs[1].Status != core.StatusFailed {
		t.Errorf("messages = %+v, want failed assistant", msgs)
	}
	evs := f.rec.all()
	if _, ok := evs[len(evs)-1].(event.RunFailed); !ok {
		t.Errorf("last event = %#v, want RunFailed", evs[len(evs)-1])
	}
}

func TestRunner_MessagesOrderedWithinSameMillisecond(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "echo", `{}`)),
		llmtest.Text("done"),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := f.messages()
	if len(msgs) != 4 || msgs[0].Role != core.RoleUser {
		t.Fatalf("messages = %+v", msgs)
	}
	if msgs[1].Parts[0].Call.ID != "c1" || msgs[2].Parts[0].Call.ID != "c2" || textOf(msgs[3]) != "done" {
		t.Errorf("messages out of order: %+v", msgs)
	}
}

func TestProxy_SetTwicePanics(t *testing.T) {
	var p Proxy
	p.Set(NewRunner(Deps{}))
	defer func() {
		if recover() == nil {
			t.Fatal("second Set did not panic")
		}
	}()
	p.Set(NewRunner(Deps{}))
}

func TestProxy_RunDelegates(t *testing.T) {
	var p Proxy
	f := newFixture(t, llmtest.New(llmtest.Text("via proxy")))
	if _, err := p.Run(context.Background(), f.rc, "hi"); !errors.Is(err, ErrProxyUnset) {
		t.Fatalf("unset Run err = %v, want ErrProxyUnset", err)
	}
	p.Set(NewRunner(f.deps))
	got, err := p.Run(context.Background(), f.rc, "hi")
	if err != nil || textOf(got) != "via proxy" {
		t.Fatalf("Run = %+v, %v", got, err)
	}
}
