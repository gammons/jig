package task

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/logtest"
)

func logTool(t *testing.T, runner *fakeRunner) (ext.Tool, *logtest.Buffer) {
	t.Helper()
	sub := exploreAgent(core.ModelRef{Provider: "anthropic", Model: "haiku"})
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	log, buf := logtest.New()
	return New(&fakeSessions{nextID: "child1"}, agentsSvc, runner, &recordingPublisher{}, testClock(), log), buf
}

func mustContain(t *testing.T, line string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(line, s) {
			t.Errorf("line %q missing %q", line, s)
		}
	}
}

func TestTaskLog_SpawnAndEnd(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "anthropic", Model: "haiku"})
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	log, buf := logtest.New()
	clk := testClock()
	runner := &fakeRunner{runFn: func(context.Context, ext.RunContext, string) (core.Message, error) {
		clk.Advance(1500 * time.Millisecond)
		return core.Message{Parts: []core.Part{{Kind: core.PartText, Text: "done"}}}, nil
	}}
	tool := New(&fakeSessions{nextID: "child1"}, agentsSvc, runner, &recordingPublisher{}, clk, log)

	ctx := core.WithLogAttrs(context.Background(), slog.String("session", "parent1"))
	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work", Depth: 0}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p"})
	if _, err := tool.Run(ctx, rc, call); err != nil {
		t.Fatal(err)
	}

	spawn := buf.Find("task spawn")
	if len(spawn) != 1 {
		t.Fatalf("task spawn lines = %v", spawn)
	}
	mustContain(t, spawn[0], "cat=task", "session=parent1", "parent=parent1", "child=child1",
		"resumed=false", "subagent=explore", "model=anthropic/haiku", "depth=1")

	end := buf.Find("task end")
	if len(end) != 1 {
		t.Fatalf("task end lines = %v", end)
	}
	mustContain(t, end[0], "cat=task", "child=child1", "outcome=ok", "dur=1.5s")
	if strings.Contains(end[0], "err=") {
		t.Errorf("ok end has err: %q", end[0])
	}
}

func TestTaskLog_ChildErrorIsRaw(t *testing.T) {
	runner := &fakeRunner{runFn: func(context.Context, ext.RunContext, string) (core.Message, error) {
		return core.Message{}, errors.New("upstream proxy error: 502")
	}}
	tool, buf := logTool(t, runner)

	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work"}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p"})
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("want error result")
	}

	end := buf.Find("task end")
	if len(end) != 1 {
		t.Fatalf("task end lines = %v", end)
	}
	mustContain(t, end[0], "outcome=error", `err="upstream proxy error: 502"`)
}

func TestTaskLog_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{runFn: func(context.Context, ext.RunContext, string) (core.Message, error) {
		cancel()
		return core.Message{}, context.Canceled
	}}
	tool, buf := logTool(t, runner)

	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work"}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p"})
	if _, err := tool.Run(ctx, rc, call); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	end := buf.Find("task end")
	if len(end) != 1 {
		t.Fatalf("task end lines = %v", end)
	}
	mustContain(t, end[0], "outcome=cancelled", "err=")
}

func TestTaskLog_Resumed(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "anthropic", Model: "haiku"})
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	sessions := &fakeSessions{sessions: map[core.SessionID]core.Session{
		"child9": {ID: "child9", ParentID: "parent1"},
	}}
	log, buf := logtest.New()
	tool := New(sessions, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), log)

	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work"}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p", "session_id": "child9"})
	if _, err := tool.Run(context.Background(), rc, call); err != nil {
		t.Fatal(err)
	}
	spawn := buf.Find("task spawn")
	if len(spawn) != 1 {
		t.Fatalf("task spawn lines = %v", spawn)
	}
	mustContain(t, spawn[0], "child=child9", "resumed=true")
}

func TestTaskLog_DepthRejected(t *testing.T) {
	tool, buf := logTool(t, &fakeRunner{})

	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work", Depth: MaxDepth}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p"})
	if _, err := tool.Run(context.Background(), rc, call); err != nil {
		t.Fatal(err)
	}

	rej := buf.Find("task rejected")
	if len(rej) != 1 {
		t.Fatalf("task rejected lines = %v", rej)
	}
	mustContain(t, rej[0], "cat=task", `reason="subagent depth limit (3) reached"`)
	if got := buf.Find("task spawn"); len(got) != 0 {
		t.Errorf("unexpected spawn: %v", got)
	}
}

func TestTaskLog_UnknownAgentAndModelRejected(t *testing.T) {
	sub := exploreAgent(core.ModelRef{})
	log, buf := logtest.New()
	agentsSvc := &fakeAgents{
		byName: map[string]core.Agent{"explore": sub},
		subs:   []core.Agent{sub},
		resolveModel: func(core.Agent, core.ModelRef, core.ModelRef) (core.ModelRef, error) {
			return core.ModelRef{}, errors.New("no model configured")
		},
	}
	tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), log)
	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work"}

	if _, err := tool.Run(context.Background(), rc, mustTaskCall(t, map[string]string{"agent": "nope", "description": "d", "prompt": "p"})); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), rc, mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p"})); err != nil {
		t.Fatal(err)
	}
	rej := buf.Find("task rejected")
	if len(rej) != 2 {
		t.Fatalf("task rejected lines = %v", rej)
	}
	mustContain(t, rej[0], `reason="unknown subagent \"nope\"; available: explore"`)
	mustContain(t, rej[1], `reason="no model configured"`)
}

func TestTaskLog_ChildResolveRejected(t *testing.T) {
	sub := exploreAgent(core.ModelRef{Provider: "anthropic", Model: "haiku"})
	agentsSvc := &fakeAgents{byName: map[string]core.Agent{"explore": sub}, subs: []core.Agent{sub}}
	log, buf := logtest.New()
	tool := New(&fakeSessions{}, agentsSvc, &fakeRunner{}, &recordingPublisher{}, testClock(), log)

	rc := ext.RunContext{SessionID: "parent1", WorkDir: "/work"}
	call := mustTaskCall(t, map[string]string{"agent": "explore", "description": "d", "prompt": "p", "session_id": "ghost"})
	if _, err := tool.Run(context.Background(), rc, call); err != nil {
		t.Fatal(err)
	}
	rej := buf.Find("task rejected")
	if len(rej) != 1 {
		t.Fatalf("task rejected lines = %v", rej)
	}
	mustContain(t, rej[0], "is not a subagent session of this session")
}
