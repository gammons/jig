package agent

import (
	"context"
	"crypto/rand"
	"path/filepath"
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

// newEventsFixture wires a Deps around a real store in t.TempDir(), a fake
// clock, and an event recorder, for a session named sessionID with model
// info info. It mirrors newFixture but lets the caller pick a session ID
// distinct from the root session ID, and the model's cost info.
func newEventsFixture(t *testing.T, sessionID core.SessionID, info core.ModelInfo, llm core.LLM, regs ...func(*ext.Registry)) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "jig.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := st.CreateSession(ctx, core.Session{ID: sessionID, CreatedAt: clk.Now(), UpdatedAt: clk.Now()}); err != nil {
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
			LLMs:     fakeSource{llm: llm, info: info},
			Ext:      reg.Freeze(),
			Store:    st,
			History:  storeHistory{st},
			Bus:      rec,
			Clock:    clk,
			IDs:      ids.New(clk, rand.Reader),
			ToolsFor: agents.ToolsFor,
		},
		rc: ext.RunContext{
			SessionID: sessionID,
			Agent:     core.Agent{Name: "build"},
			Model:     model,
			WorkDir:   t.TempDir(),
		},
		model: model,
	}
}

// TestRunner_EventsCarryRootID pins that every event a run publishes,
// across a child session distinct from its root, carries both the run's
// session and its root session.
func TestRunner_EventsCarryRootID(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Text("done"),
	)
	f := newEventsFixture(t, "ses_c", testInfo(), llm, withTools(echoTool{name: "echo"}))
	f.rc.RootID = "ses_r"
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	evs := f.rec.all()
	if len(evs) == 0 {
		t.Fatal("no events published")
	}
	for i, e := range evs {
		if e.Root() != core.SessionID("ses_r") {
			t.Errorf("event %d (%T) Root() = %q, want %q", i, e, e.Root(), "ses_r")
		}
		if e.Session() != core.SessionID("ses_c") {
			t.Errorf("event %d (%T) Session() = %q, want %q", i, e, e.Session(), "ses_c")
		}
	}
}

// TestRunner_PublishesStepFinishedPerStep pins that StepFinished is
// published once per completed step, carrying that step's assistant
// message ID, usage, and cost.
func TestRunner_PublishesStepFinishedPerStep(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Text("done"),
	)
	info := core.ModelInfo{DefaultMaxTokens: 4096, CostIn: 1, CostOut: 1}
	f := newEventsFixture(t, "ses_step", info, llm, withTools(echoTool{name: "echo"}))
	f.rc.RootID = f.rc.SessionID
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := f.messages()
	if len(msgs) != 3 {
		t.Fatalf("stored %d messages, want 3 (user + 2 assistant steps)", len(msgs))
	}
	wantIDs := []core.MessageID{msgs[1].ID, msgs[2].ID}

	var got []event.StepFinished
	for _, e := range f.rec.all() {
		if sf, ok := e.(event.StepFinished); ok {
			got = append(got, sf)
		}
	}
	if len(got) != 2 {
		t.Fatalf("StepFinished events = %d, want 2 (got %#v)", len(got), got)
	}
	for i, sf := range got {
		if sf.MessageID != wantIDs[i] {
			t.Errorf("StepFinished[%d].MessageID = %q, want %q", i, sf.MessageID, wantIDs[i])
		}
		if sf.Usage != (core.Usage{Input: 10, Output: 5}) {
			t.Errorf("StepFinished[%d].Usage = %+v, want {10 5}", i, sf.Usage)
		}
		if !approxEqual(sf.CostUSD, 15e-6) {
			t.Errorf("StepFinished[%d].CostUSD = %v, want 15e-6", i, sf.CostUSD)
		}
	}
}
