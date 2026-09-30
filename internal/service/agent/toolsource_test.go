package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
	"github.com/gammons/jig/internal/core/llmtest"
)

// fakeToolSource is an ext.ToolSource stub that returns the next slice in
// sequence on each call to Tools, repeating the last entry once exhausted.
type fakeToolSource struct {
	mu       sync.Mutex
	sequence [][]ext.Tool
	calls    int
}

func (s *fakeToolSource) Tools() []ext.Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	if idx >= len(s.sequence) {
		idx = len(s.sequence) - 1
	}
	s.calls++
	return s.sequence[idx]
}

func withToolSource(src ext.ToolSource) func(*ext.Registry) {
	return func(r *ext.Registry) {
		if err := r.SetToolSource(src); err != nil {
			panic(err)
		}
	}
}

func hasToolName(specs []core.ToolSpec, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func toolResultsByCallID(msgs []core.Message) map[string]*core.ToolResult {
	out := make(map[string]*core.ToolResult)
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == core.PartToolResult && p.Result != nil {
				out[p.Result.CallID] = p.Result
			}
		}
	}
	return out
}

// TestRunner_ToolSourceRecomputedPerStep pins that the Runner recomputes
// its allowed tools at the start of every step: a tool the ToolSource adds
// between steps 1 and 2 is absent from step 1's request and present, and
// callable, in step 2's.
func TestRunner_ToolSourceRecomputedPerStep(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "mcp__s__t", `{}`)),
		llmtest.Text("done"),
	)
	src := &fakeToolSource{sequence: [][]ext.Tool{
		nil,
		{echoTool{name: "mcp__s__t"}},
	}}
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}), withToolSource(src))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := llm.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	if hasToolName(reqs[0].Tools, "mcp__s__t") {
		t.Errorf("step 1 request Tools = %+v, want no mcp__s__t yet", reqs[0].Tools)
	}
	if !hasToolName(reqs[1].Tools, "mcp__s__t") {
		t.Errorf("step 2 request Tools = %+v, want mcp__s__t", reqs[1].Tools)
	}

	results := toolResultsByCallID(f.messages())
	res, ok := results["c2"]
	if !ok || res.IsError {
		t.Errorf("call c2 result = %+v, want a successful call to mcp__s__t", res)
	}
}

// TestRunner_ToolSourceDuplicateNameDropped pins that a source tool whose
// name collides with a built-in is dropped, with the built-in winning.
func TestRunner_ToolSourceDuplicateNameDropped(t *testing.T) {
	llm := llmtest.New(llmtest.Text("ok"))
	src := &fakeToolSource{sequence: [][]ext.Tool{{echoTool{name: "read"}}}}
	f := newFixture(t, llm, withTools(echoTool{name: "read"}), withToolSource(src))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := llm.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	count := 0
	for _, spec := range reqs[0].Tools {
		if spec.Name == "read" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("tool %q appears %d times in request Tools, want 1", "read", count)
	}
}

// TestRunner_ToolVanishedMidStep pins that a step's tool snapshot is fixed
// at the step's start: a tool present when the step began still runs even
// if the source stops offering it before the call executes, but a later
// step that no longer sees it gets the "unavailable" error.
func TestRunner_ToolVanishedMidStep(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "mcp__s__t", `{}`)),
		llmtest.Calls(llmtest.Call("c2", "mcp__s__t", `{}`)),
		llmtest.Text("done"),
	)
	src := &fakeToolSource{sequence: [][]ext.Tool{
		{echoTool{name: "mcp__s__t"}},
		nil,
	}}
	f := newFixture(t, llm, withToolSource(src))
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResultsByCallID(f.messages())
	res1, ok1 := results["c1"]
	if !ok1 || res1.IsError {
		t.Errorf("call c1 result = %+v, want success (step 1's snapshot still had the tool)", res1)
	}
	res2, ok2 := results["c2"]
	if !ok2 || !res2.IsError {
		t.Errorf("call c2 result = %+v, want IsError (tool vanished before step 2)", res2)
	}
}
