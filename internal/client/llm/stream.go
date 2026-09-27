package llm

import (
	"context"
	"encoding/json"
	"iter"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

// FromFantasy converts a fantasy.StreamResponse into a core.StreamEvent
// sequence: text/reasoning deltas, tool calls, and the final usage/finish
// reason carry across; every other fantasy.StreamPartType is ignored. A
// "error" part is mapped with MapError and ends the sequence.
func FromFantasy(parts fantasy.StreamResponse) iter.Seq2[core.StreamEvent, error] {
	return func(yield func(core.StreamEvent, error) bool) {
		for p := range parts {
			ev, err, ok := convertStreamPart(p)
			if !ok {
				continue
			}
			if !yield(ev, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// convertStreamPart converts one fantasy.StreamPart into a core.StreamEvent.
// ok is false for a StreamPartType FromFantasy ignores.
func convertStreamPart(p fantasy.StreamPart) (core.StreamEvent, error, bool) {
	switch p.Type {
	case fantasy.StreamPartTypeTextDelta:
		return core.StreamEvent{Kind: core.StreamText, Text: p.Delta}, nil, true
	case fantasy.StreamPartTypeReasoningDelta:
		return core.StreamEvent{Kind: core.StreamReasoning, Text: p.Delta}, nil, true
	case fantasy.StreamPartTypeToolCall:
		call := core.ToolCall{ID: p.ID, Name: p.ToolCallName, Input: json.RawMessage(p.ToolCallInput)}
		return core.StreamEvent{Kind: core.StreamToolCall, Call: &call}, nil, true
	case fantasy.StreamPartTypeFinish:
		return core.StreamEvent{
			Kind:         core.StreamFinish,
			Usage:        convertUsage(p.Usage),
			FinishReason: string(p.FinishReason),
		}, nil, true
	case fantasy.StreamPartTypeError:
		return core.StreamEvent{}, MapError(p.Error), true
	default:
		return core.StreamEvent{}, nil, false
	}
}

// convertUsage maps fantasy's Usage fields onto core.Usage: InputTokens and
// OutputTokens carry across directly, CacheReadTokens becomes CacheRead,
// and CacheCreationTokens becomes CacheWrite.
func convertUsage(u fantasy.Usage) core.Usage {
	return core.Usage{
		Input:      u.InputTokens,
		Output:     u.OutputTokens,
		CacheRead:  u.CacheReadTokens,
		CacheWrite: u.CacheCreationTokens,
	}
}

// adapter is the core.LLM built from a fantasy.LanguageModel.
type adapter struct {
	lm fantasy.LanguageModel
}

// Stream implements core.LLM. An error returned by the underlying model
// before any StreamPart (e.g. an HTTP 429) is mapped with MapError and
// yielded as the sequence's only event.
func (a *adapter) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	call := ToFantasy(req)
	parts, err := a.lm.Stream(ctx, call)
	if err != nil {
		return func(yield func(core.StreamEvent, error) bool) {
			yield(core.StreamEvent{}, MapError(err))
		}
	}
	return FromFantasy(parts)
}
