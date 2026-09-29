package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gammons/jig/internal/clock"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
)

func testClock() *clock.Fake {
	return clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

// fakeSessions is a scripted Sessions for task tests.
type fakeSessions struct {
	createErr error
	getErr    error
	getFn     func(ctx context.Context, id core.SessionID) (core.Session, error)
	sessions  map[core.SessionID]core.Session
	nextID    core.SessionID

	gotParent core.SessionID
	gotAgent  string
	gotModel  core.ModelRef
	gotTitle  string
}

func (f *fakeSessions) CreateChild(_ context.Context, parent core.SessionID, agent string, model core.ModelRef, title string) (core.Session, error) {
	f.gotParent, f.gotAgent, f.gotModel, f.gotTitle = parent, agent, model, title
	if f.createErr != nil {
		return core.Session{}, f.createErr
	}
	id := f.nextID
	if id == "" {
		id = "child1"
	}
	return core.Session{ID: id, ParentID: parent, Agent: agent}, nil
}

func (f *fakeSessions) Get(ctx context.Context, id core.SessionID) (core.Session, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	if f.getErr != nil {
		return core.Session{}, f.getErr
	}
	s, ok := f.sessions[id]
	if !ok {
		return core.Session{}, errors.New("session not found")
	}
	return s, nil
}

// fakeAgents is a scripted Agents for task tests.
type fakeAgents struct {
	byName       map[string]core.Agent
	subs         []core.Agent
	resolveModel func(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error)
}

func (f *fakeAgents) Get(name string) (core.Agent, bool) {
	a, ok := f.byName[name]
	return a, ok
}

func (f *fakeAgents) Subagents() []core.Agent { return f.subs }

func (f *fakeAgents) ResolveModel(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error) {
	if f.resolveModel != nil {
		return f.resolveModel(a, parent, session)
	}
	if !a.Model.IsZero() {
		return a.Model, nil
	}
	if !parent.IsZero() {
		return parent, nil
	}
	return session, nil
}

// fakeRunner is a scripted Runner for task tests.
type fakeRunner struct {
	runFn func(ctx context.Context, rc ext.RunContext, text string) (core.Message, error)

	gotRC   ext.RunContext
	gotText string
	called  bool
}

func (f *fakeRunner) Run(ctx context.Context, rc ext.RunContext, text string, _ ...core.Attachment) (core.Message, error) {
	f.called = true
	f.gotRC, f.gotText = rc, text
	if f.runFn != nil {
		return f.runFn(ctx, rc, text)
	}
	return core.Message{Parts: []core.Part{{Kind: core.PartText, Text: "ok"}}}, nil
}

// recordingPublisher records every event Published to it.
type recordingPublisher struct {
	events []event.Event
}

func (r *recordingPublisher) Publish(e event.Event) { r.events = append(r.events, e) }

func mustTaskCall(t *testing.T, input any) core.ToolCall {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return core.ToolCall{ID: "c1", Name: "task", Input: raw}
}

func exploreAgent(model core.ModelRef) core.Agent {
	return core.Agent{
		Name:        "explore",
		Description: "Read-only subagent for investigating the codebase.",
		Mode:        core.ModeSubagent,
		Model:       model,
	}
}

func TestTask_SpawnsChildWithResolvedModel(t *testing.T) {
	haiku := core.ModelRef{Provider: "anthropic", Model: "haiku"}
	opus := core.ModelRef{Provider: "anthropic", Model: "opus"}
	sub := exploreAgent(haiku)

	sessions := &fakeSessions{nextID: "child1"}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	runner := &fakeRunner{}
	pub := &recordingPublisher{}
	tool := New(sessions, agentsSvc, runner, pub, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1", Model: opus, WorkDir: "/work", Depth: 1}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "look around", "prompt": "find X"})

	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	if sessions.gotModel != haiku {
		t.Errorf("CreateChild model = %v, want %v", sessions.gotModel, haiku)
	}
	if sessions.gotParent != "parent1" {
		t.Errorf("CreateChild parent = %q, want %q", sessions.gotParent, "parent1")
	}
	if sessions.gotAgent != "explore" {
		t.Errorf("CreateChild agent = %q, want %q", sessions.gotAgent, "explore")
	}
	if sessions.gotTitle != "look around" {
		t.Errorf("CreateChild title = %q, want %q", sessions.gotTitle, "look around")
	}

	if runner.gotRC.Model != haiku {
		t.Errorf("child rc.Model = %v, want %v", runner.gotRC.Model, haiku)
	}
	if runner.gotRC.SessionID != "child1" {
		t.Errorf("child rc.SessionID = %q, want %q", runner.gotRC.SessionID, "child1")
	}
	if runner.gotRC.RootID != "parent1" {
		t.Errorf("child rc.RootID = %q, want %q (falls back to parent SessionID)", runner.gotRC.RootID, "parent1")
	}
	if runner.gotRC.WorkDir != "/work" {
		t.Errorf("child rc.WorkDir = %q, want %q", runner.gotRC.WorkDir, "/work")
	}
	if runner.gotRC.Depth != 2 {
		t.Errorf("child rc.Depth = %d, want 2", runner.gotRC.Depth)
	}
	if runner.gotRC.Agent.Name != "explore" {
		t.Errorf("child rc.Agent.Name = %q, want %q", runner.gotRC.Agent.Name, "explore")
	}
	if runner.gotText != "find X" {
		t.Errorf("child prompt = %q, want %q", runner.gotText, "find X")
	}

	wantOut := `<task_result session_id="child1">` + "\n" + "ok" + "\n" + `</task_result>`
	if res.Output != wantOut {
		t.Errorf("Output = %q, want %q", res.Output, wantOut)
	}

	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	sp, ok := pub.events[0].(event.SubagentSpawned)
	if !ok {
		t.Fatalf("event type = %T, want event.SubagentSpawned", pub.events[0])
	}
	if sp.Session() != "parent1" || sp.Child != "child1" || sp.Agent != "explore" || sp.Description != "look around" {
		t.Errorf("SubagentSpawned = %+v, unexpected", sp)
	}
	if sp.CallID != call.ID {
		t.Errorf("SubagentSpawned.CallID = %q, want %q", sp.CallID, call.ID)
	}
	if sp.RootID != rc.RootID {
		t.Errorf("SubagentSpawned.RootID = %q, want %q", sp.RootID, rc.RootID)
	}
}

func TestTask_InheritsParentModelWhenUnset(t *testing.T) {
	opus := core.ModelRef{Provider: "anthropic", Model: "opus"}
	sub := exploreAgent(core.ModelRef{}) // no model of its own

	sessions := &fakeSessions{}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	runner := &fakeRunner{}
	pub := &recordingPublisher{}
	tool := New(sessions, agentsSvc, runner, pub, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1", Model: opus}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})

	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if runner.gotRC.Model != opus {
		t.Errorf("child rc.Model = %v, want %v (inherited from parent)", runner.gotRC.Model, opus)
	}
}

func TestTask_ModelResolveError(t *testing.T) {
	sub := exploreAgent(core.ModelRef{})
	wantErr := errors.New("no model configured")

	sessions := &fakeSessions{}
	agentsSvc := &fakeAgents{
		byName: map[string]core.Agent{"explore": sub},
		subs:   []core.Agent{sub},
		resolveModel: func(a core.Agent, parent, session core.ModelRef) (core.ModelRef, error) {
			return core.ModelRef{}, wantErr
		},
	}
	runner := &fakeRunner{}
	pub := &recordingPublisher{}
	tool := New(sessions, agentsSvc, runner, pub, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1"}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	if res.Output != wantErr.Error() {
		t.Errorf("Output = %q, want %q", res.Output, wantErr.Error())
	}
	if runner.called {
		t.Error("Runner.Run was called, want not called")
	}
}

func TestTask_DepthLimit(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	sessions := &fakeSessions{}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	runner := &fakeRunner{}
	pub := &recordingPublisher{}
	tool := New(sessions, agentsSvc, runner, pub, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1", Depth: MaxDepth}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	want := "subagent depth limit (3) reached"
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
	if sessions.gotAgent != "" {
		t.Error("CreateChild was called, want not called")
	}
	if runner.called {
		t.Error("Runner.Run was called, want not called")
	}
}

func TestTask_UnknownAgentListsAvailable(t *testing.T) {
	explore := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	general := core.Agent{Name: "general", Description: "General-purpose subagent.", Mode: core.ModeSubagent}
	primaryOnly := core.Agent{Name: "build", Description: "Primary agent.", Mode: core.ModePrimary}

	t.Run("unknown name", func(t *testing.T) {
		agentsSvc := &fakeAgents{
			byName: map[string]core.Agent{"explore": explore, "general": general},
			subs:   []core.Agent{general, explore}, // deliberately unsorted
		}
		tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), nil)
		call := mustTaskCall(t, map[string]any{"agent": "bogus", "description": "d", "prompt": "p"})
		res, err := tool.Run(context.Background(), ext.RunContext{SessionID: "p1"}, call)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !res.IsError {
			t.Fatal("IsError = false, want true")
		}
		want := `unknown subagent "bogus"; available: explore, general`
		if res.Output != want {
			t.Errorf("Output = %q, want %q", res.Output, want)
		}
	})

	t.Run("non-subagent name", func(t *testing.T) {
		agentsSvc := &fakeAgents{
			byName: map[string]core.Agent{"build": primaryOnly, "explore": explore},
			subs:   []core.Agent{explore},
		}
		tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), nil)
		call := mustTaskCall(t, map[string]any{"agent": "build", "description": "d", "prompt": "p"})
		res, err := tool.Run(context.Background(), ext.RunContext{SessionID: "p1"}, call)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !res.IsError {
			t.Fatal("IsError = false, want true")
		}
		want := `unknown subagent "build"; available: explore`
		if res.Output != want {
			t.Errorf("Output = %q, want %q", res.Output, want)
		}
	})
}

func TestTask_ContinueRequiresOwnChild(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})

	t.Run("valid continuation", func(t *testing.T) {
		sessions := &fakeSessions{sessions: map[core.SessionID]core.Session{
			"child9": {ID: "child9", ParentID: "parent1"},
		}}
		agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
		runner := &fakeRunner{}
		pub := &recordingPublisher{}
		tool := New(sessions, agentsSvc, runner, pub, testClock(), nil)

		rc := ext.RunContext{SessionID: "parent1", RootID: "root1"}
		call := mustTaskCall(t, map[string]any{
			"agent": "explore", "description": "d", "prompt": "continue please", "session_id": "child9",
		})
		res, err := tool.Run(context.Background(), rc, call)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.IsError {
			t.Fatalf("IsError: %s", res.Output)
		}
		if runner.gotRC.SessionID != "child9" {
			t.Errorf("child rc.SessionID = %q, want %q", runner.gotRC.SessionID, "child9")
		}
		if sessions.gotAgent != "" {
			t.Error("CreateChild was called on a continuation, want not called")
		}

		if len(pub.events) != 1 {
			t.Fatalf("published %d events, want 1", len(pub.events))
		}
		sp, ok := pub.events[0].(event.SubagentSpawned)
		if !ok {
			t.Fatalf("event type = %T, want event.SubagentSpawned", pub.events[0])
		}
		if sp.Session() != "parent1" || sp.Child != "child9" || sp.Agent != "explore" || sp.Description != "d" {
			t.Errorf("SubagentSpawned = %+v, want Session=parent1 Child=child9 Agent=explore Description=d", sp)
		}
		if sp.CallID != call.ID {
			t.Errorf("SubagentSpawned.CallID = %q, want %q", sp.CallID, call.ID)
		}
		if sp.RootID != rc.RootID {
			t.Errorf("SubagentSpawned.RootID = %q, want %q", sp.RootID, rc.RootID)
		}
	})

	t.Run("wrong parent", func(t *testing.T) {
		sessions := &fakeSessions{sessions: map[core.SessionID]core.Session{
			"child9": {ID: "child9", ParentID: "someone-else"},
		}}
		agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
		runner := &fakeRunner{}
		tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

		rc := ext.RunContext{SessionID: "parent1"}
		call := mustTaskCall(t, map[string]any{
			"agent": "explore", "description": "d", "prompt": "continue please", "session_id": "child9",
		})
		res, err := tool.Run(context.Background(), rc, call)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !res.IsError {
			t.Fatal("IsError = false, want true")
		}
		want := `session "child9" is not a subagent session of this session`
		if res.Output != want {
			t.Errorf("Output = %q, want %q", res.Output, want)
		}
		if runner.called {
			t.Error("Runner.Run was called, want not called")
		}
	})

	t.Run("session_id does not exist", func(t *testing.T) {
		sessions := &fakeSessions{sessions: map[core.SessionID]core.Session{}}
		agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
		runner := &fakeRunner{}
		tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

		rc := ext.RunContext{SessionID: "parent1"}
		call := mustTaskCall(t, map[string]any{
			"agent": "explore", "description": "d", "prompt": "continue please", "session_id": "nope",
		})
		res, err := tool.Run(context.Background(), rc, call)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !res.IsError {
			t.Fatal("IsError = false, want true")
		}
		want := `session "nope" is not a subagent session of this session`
		if res.Output != want {
			t.Errorf("Output = %q, want %q (same fixed message as wrong-parent, not the raw store error)", res.Output, want)
		}
		if runner.called {
			t.Error("Runner.Run was called, want not called")
		}
	})

	t.Run("session_id lookup fails while ctx is done", func(t *testing.T) {
		sessions := &fakeSessions{}
		ctx, cancel := context.WithCancel(context.Background())
		sessions.getFn = func(ctx context.Context, id core.SessionID) (core.Session, error) {
			cancel() // simulate the ctx being cancelled during the Get call itself
			return core.Session{}, ctx.Err()
		}
		agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
		runner := &fakeRunner{}
		tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

		rc := ext.RunContext{SessionID: "parent1"}
		call := mustTaskCall(t, map[string]any{
			"agent": "explore", "description": "d", "prompt": "continue please", "session_id": "child9",
		})
		_, err := tool.Run(ctx, rc, call)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if runner.called {
			t.Error("Runner.Run was called, want not called")
		}
	})
}

func TestTask_ParentCancelCancelsChild(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	sessions := &fakeSessions{}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}

	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{runFn: func(ctx context.Context, rc ext.RunContext, text string) (core.Message, error) {
		cancel() // simulate the parent being cancelled mid-run
		<-ctx.Done()
		return core.Message{}, ctx.Err()
	}}
	tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1"}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	_, err := tool.Run(ctx, rc, call)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestTask_ChildErrorIsWrapped(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	sessions := &fakeSessions{nextID: "child1"}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	runner := &fakeRunner{runFn: func(ctx context.Context, rc ext.RunContext, text string) (core.Message, error) {
		return core.Message{}, errors.New("boom")
	}}
	tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1"}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	want := `<task_result session_id="child1">` + "\n" + "error: boom" + "\n" + `</task_result>`
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
}

// TestTask_ChildBusyErrorIsWrapped pins that Run's IsError-wrapping of a
// child error is generic: any error the Runner returns while the parent
// ctx is still live (not just a plain error, but e.g. an ErrBusy-style
// sentinel from a real agent.Runner) becomes the IsError wrapper, never a
// Go-level error.
func TestTask_ChildBusyErrorIsWrapped(t *testing.T) {
	busyErr := errors.New("agent: session already has a running turn")
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	sessions := &fakeSessions{nextID: "child1"}
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	runner := &fakeRunner{runFn: func(ctx context.Context, rc ext.RunContext, text string) (core.Message, error) {
		return core.Message{}, busyErr
	}}
	tool := New(sessions, agentsSvc, runner, &recordingPublisher{}, testClock(), nil)

	rc := ext.RunContext{SessionID: "parent1"}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true")
	}
	want := `<task_result session_id="child1">` + "\n" + "error: agent: session already has a running turn" + "\n" + `</task_result>`
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
}

func TestTask_DescriptionListsSubagents(t *testing.T) {
	subs := []core.Agent{
		{Name: "explore", Description: "Read-only subagent for investigating the codebase."},
		{Name: "general", Description: "General-purpose subagent with full tool access."},
	}
	agentsSvc := &fakeAgents{subs: subs}
	tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), nil)

	want := "Launch a subagent to handle a task autonomously.\n\n" +
		"Available agents:\n" +
		"- explore: Read-only subagent for investigating the codebase.\n" +
		"- general: General-purpose subagent with full tool access.\n"
	if got := tool.Description(); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func TestTask_MissingRequiredFields(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "a", Model: "m"})
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}

	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"missing agent", map[string]any{"description": "d", "prompt": "p"}, "agent is required"},
		{"missing description", map[string]any{"agent": "explore", "prompt": "p"}, "description is required"},
		{"missing prompt", map[string]any{"agent": "explore", "description": "d"}, "prompt is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), nil)
			call := mustTaskCall(t, tt.input)
			res, err := tool.Run(context.Background(), ext.RunContext{SessionID: "p1"}, call)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !res.IsError {
				t.Fatal("IsError = false, want true")
			}
			if res.Output != tt.want {
				t.Errorf("Output = %q, want %q", res.Output, tt.want)
			}
		})
	}
}

func TestTask_ChildCarriesAncestorPermissions(t *testing.T) {
	sub := exploreAgent(core.ModelRef{})
	runner := &fakeRunner{}
	tool := New(&fakeSessions{}, &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}, runner, &recordingPublisher{}, testClock(), nil)

	grand := core.PermissionRules{"bash": {Default: core.Deny}}
	parent := core.PermissionRules{"bash": {Default: core.Ask}}
	ancestors := make([]core.PermissionRules, 1, 4)
	ancestors[0] = grand
	rc := ext.RunContext{
		SessionID: "p1",
		Agent:     core.Agent{Name: "plan", Permissions: parent},
		Ancestors: ancestors,
		Depth:     1,
	}
	call := mustTaskCall(t, map[string]any{"agent": "explore", "description": "d", "prompt": "p"})
	if _, err := tool.Run(context.Background(), rc, call); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := runner.gotRC.Ancestors
	if len(got) != 2 {
		t.Fatalf("child Ancestors len = %d, want 2", len(got))
	}
	if got[0]["bash"].Default != core.Deny || got[1]["bash"].Default != core.Ask {
		t.Errorf("child Ancestors = %+v, want [grandparent, parent]", got)
	}
	// The child's slice must not alias the parent's spare capacity.
	if len(rc.Ancestors) != 1 || &got[0] == &ancestors[0] {
		t.Error("child Ancestors aliases the parent's slice")
	}
}
