package llm

import (
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

// seqOf builds a fantasy.StreamResponse that yields parts in order.
func seqOf(parts ...fantasy.StreamPart) fantasy.StreamResponse {
	return func(yield func(fantasy.StreamPart) bool) {
		for _, p := range parts {
			if !yield(p) {
				return
			}
		}
	}
}

func collect(seq func(func(core.StreamEvent, error) bool)) ([]core.StreamEvent, []error) {
	var events []core.StreamEvent
	var errs []error
	for ev, err := range seq {
		events = append(events, ev)
		errs = append(errs, err)
	}
	return events, errs
}

func TestFromFantasy_MapsDeltasCallsAndUsage(t *testing.T) {
	parts := seqOf(
		fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "Hello"},
		fantasy.StreamPart{Type: fantasy.StreamPartTypeReasoningDelta, Delta: "why"},
		fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "c1", ToolCallName: "weather", ToolCallInput: `{"a":1}`},
		fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 5, OutputTokens: 7, CacheReadTokens: 2, CacheCreationTokens: 3},
		},
	)

	events, errs := collect(FromFantasy(parts))

	if len(events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(events), events)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
	}

	if events[0].Kind != core.StreamText || events[0].Text != "Hello" {
		t.Errorf("event 0: got %+v, want StreamText Hello", events[0])
	}
	if events[1].Kind != core.StreamReasoning || events[1].Text != "why" {
		t.Errorf("event 1: got %+v, want StreamReasoning why", events[1])
	}
	wantCall := core.ToolCall{ID: "c1", Name: "weather", Input: json.RawMessage(`{"a":1}`)}
	if events[2].Kind != core.StreamToolCall || events[2].Call == nil ||
		events[2].Call.ID != wantCall.ID || events[2].Call.Name != wantCall.Name || string(events[2].Call.Input) != string(wantCall.Input) {
		t.Errorf("event 2: got %+v, want StreamToolCall %+v", events[2], wantCall)
	}
	wantUsage := core.Usage{Input: 5, Output: 7, CacheRead: 2, CacheWrite: 3}
	if events[3].Kind != core.StreamFinish || events[3].FinishReason != "stop" || events[3].Usage != wantUsage {
		t.Errorf("event 3: got %+v, want StreamFinish stop with usage %+v", events[3], wantUsage)
	}
}

func TestFromFantasy_ErrorStops(t *testing.T) {
	providerErr := &fantasy.ProviderError{Message: "boom", StatusCode: 500}
	parts := seqOf(
		fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "partial"},
		fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: providerErr},
		fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "unreachable"},
	)

	events, errs := collect(FromFantasy(parts))

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (text then error): %+v", len(events), events)
	}
	if errs[0] != nil {
		t.Fatalf("event 0: unexpected error: %v", errs[0])
	}
	if events[0].Text != "partial" {
		t.Errorf("event 0: got %+v, want text %q", events[0], "partial")
	}

	var llmErr *core.LLMError
	if !errors.As(errs[1], &llmErr) {
		t.Fatalf("event 1 error: got %v (%T), want *core.LLMError", errs[1], errs[1])
	}
	if !errors.Is(llmErr, providerErr) {
		t.Errorf("mapped error does not wrap the original provider error")
	}
}
