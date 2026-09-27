package llm

import (
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

func TestMapError_RetryableAndRetryAfter(t *testing.T) {
	pe := &fantasy.ProviderError{
		Message:         "rate limited",
		StatusCode:      429,
		ResponseHeaders: map[string]string{"Retry-After": "7"}, // mixed case: lookup is case-insensitive
	}

	got := MapError(pe)

	var llmErr *core.LLMError
	if !errors.As(got, &llmErr) {
		t.Fatalf("got %v (%T), want *core.LLMError", got, got)
	}
	if !llmErr.Retryable {
		t.Error("Retryable: got false, want true for a 429")
	}
	if llmErr.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter: got %v, want 7s", llmErr.RetryAfter)
	}
	if !errors.Is(llmErr, pe) {
		t.Error("mapped error does not unwrap to the original *fantasy.ProviderError")
	}
}

func TestMapError_NonProviderErrorPassesThrough(t *testing.T) {
	want := errors.New("plain error")

	got := MapError(want)

	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v unchanged", got, want)
	}
	var llmErr *core.LLMError
	if errors.As(got, &llmErr) {
		t.Errorf("got %+v, want a plain error, not a *core.LLMError", llmErr)
	}
}

func TestMapError_NoRetryAfterHeaderIsZero(t *testing.T) {
	pe := &fantasy.ProviderError{Message: "server error", StatusCode: 500}

	got := MapError(pe)

	var llmErr *core.LLMError
	if !errors.As(got, &llmErr) {
		t.Fatalf("got %v, want *core.LLMError", got)
	}
	if llmErr.RetryAfter != 0 {
		t.Errorf("RetryAfter: got %v, want 0", llmErr.RetryAfter)
	}
}
