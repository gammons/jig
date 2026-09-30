package core

import (
	"context"
	"iter"
	"time"
)

// ToolSpec describes a tool the model may call.
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}

// LLMRequest is one request to stream a completion.
type LLMRequest struct {
	Model           ModelRef
	System          []string
	Messages        []Message
	Tools           []ToolSpec
	MaxOutputTokens int64
	// Effort is the effective reasoning effort (already defaulted and
	// clamped against the model's levels); "" sends none.
	Effort Effort
}

// StreamKind distinguishes the kinds of events an LLM emits while streaming.
type StreamKind string

const (
	StreamText      StreamKind = "text"
	StreamReasoning StreamKind = "reasoning"
	StreamToolCall  StreamKind = "tool_call"
	StreamFinish    StreamKind = "finish"
)

// StreamEvent is one increment of an LLM's streamed response.
type StreamEvent struct {
	Kind         StreamKind
	Text         string
	Call         *ToolCall
	Usage        Usage
	FinishReason string
}

// LLM streams completions from a model.
type LLM interface {
	Stream(ctx context.Context, req LLMRequest) iter.Seq2[StreamEvent, error]
}

// LLMError wraps an error from an LLM provider with retry information.
type LLMError struct {
	Retryable  bool
	RetryAfter time.Duration
	Err        error
}

// Error returns the wrapped error's message, or "llm error" if Err is nil.
func (e *LLMError) Error() string {
	if e.Err == nil {
		return "llm error"
	}
	return e.Err.Error()
}

// Unwrap returns e.Err, so errors.Is/As can see through LLMError.
func (e *LLMError) Unwrap() error {
	return e.Err
}
