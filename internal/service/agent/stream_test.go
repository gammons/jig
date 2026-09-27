package agent

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestStream_CoalescesDeltasByKind(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Events: []core.StreamEvent{
		{Kind: core.StreamReasoning, Text: "think "},
		{Kind: core.StreamReasoning, Text: "hard"},
		{Kind: core.StreamText, Text: "a"},
		{Kind: core.StreamText, Text: "b"},
		{Kind: core.StreamReasoning, Text: "more"},
		{Kind: core.StreamText, Text: "c"},
		{Kind: core.StreamFinish, Usage: core.Usage{Input: 7, Output: 3}},
	}})
	f := newFixture(t, llm)
	r := NewRunner(f.deps)

	got, err := r.Run(context.Background(), f.rc, "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []core.Part{
		{Kind: core.PartReasoning, Text: "think hard"},
		{Kind: core.PartText, Text: "ab"},
		{Kind: core.PartReasoning, Text: "more"},
		{Kind: core.PartText, Text: "c"},
	}
	stored := f.messages()[1]
	for _, m := range []core.Message{got, stored} {
		if len(m.Parts) != len(want) {
			t.Fatalf("parts = %+v, want %+v", m.Parts, want)
		}
		for i := range want {
			if m.Parts[i].Kind != want[i].Kind || m.Parts[i].Text != want[i].Text {
				t.Errorf("part %d = %+v, want %+v", i, m.Parts[i], want[i])
			}
		}
	}
	if stored.Usage != (core.Usage{Input: 7, Output: 3}) {
		t.Errorf("usage = %+v, want finish usage", stored.Usage)
	}

	var text, reasoning string
	for _, e := range f.rec.all() {
		switch v := e.(type) {
		case event.TextDelta:
			text += v.Text
		case event.ReasoningDelta:
			reasoning += v.Text
		}
	}
	if text != "abc" || reasoning != "think hardmore" {
		t.Errorf("published text=%q reasoning=%q", text, reasoning)
	}
}

func TestStream_SavesOnFirstToolCall(t *testing.T) {
	llm := llmtest.New(
		llmtest.Calls(llmtest.Call("c1", "echo", `{}`), llmtest.Call("c2", "echo", `{}`)),
		llmtest.Text("done"),
	)
	f := newFixture(t, llm, withTools(echoTool{name: "echo"}))
	ns := notifyStore{s: f.st, saved: make(chan core.Message, 32)}
	f.deps.Store = ns
	r := NewRunner(f.deps)

	if _, err := r.Run(context.Background(), f.rc, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ns.saved)
	var streaming []core.Message
	for m := range ns.saved {
		if m.Status == core.StatusStreaming {
			streaming = append(streaming, m)
		}
	}
	if len(streaming) != 1 || len(streaming[0].Parts) != 1 || streaming[0].Parts[0].Call.ID != "c1" {
		t.Errorf("streaming saves = %+v, want exactly one at the first tool call", streaming)
	}
}
