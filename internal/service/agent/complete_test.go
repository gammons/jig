package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/llmtest"
)

func TestComplete_JoinsText(t *testing.T) {
	llm := llmtest.New(llmtest.Turn{Events: []core.StreamEvent{
		{Kind: core.StreamReasoning, Text: "thinking"},
		{Kind: core.StreamText, Text: "Hello, "},
		{Kind: core.StreamText, Text: "world"},
		{Kind: core.StreamFinish, FinishReason: "stop"},
	}})

	got, err := Complete(context.Background(), llm, "be brief", "say hi")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello, world" {
		t.Errorf("Complete = %q, want %q", got, "Hello, world")
	}

	reqs := llm.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if len(req.System) != 1 || req.System[0] != "be brief" {
		t.Errorf("System = %q, want [be brief]", req.System)
	}
	if len(req.Tools) != 0 {
		t.Errorf("Tools = %v, want none", req.Tools)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != core.RoleUser ||
		len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Text != "say hi" {
		t.Errorf("Messages = %+v, want one user message %q", req.Messages, "say hi")
	}
}

func TestComplete_StreamError(t *testing.T) {
	boom := errors.New("boom")
	llm := llmtest.New(llmtest.Turn{
		Events: []core.StreamEvent{{Kind: core.StreamText, Text: "partial"}},
		Err:    boom,
	})
	if _, err := Complete(context.Background(), llm, "s", "u"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}
