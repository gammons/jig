package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

// serveFixture returns an httptest.Server that responds to every request
// with the SSE fixture at path, as text/event-stream.
func serveFixture(t *testing.T, path string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

func TestAnthropicFactory_StreamsFromFixture(t *testing.T) {
	srv := serveFixture(t, "testdata/anthropic/text_and_tool.sse")
	defer srv.Close()

	factory := anthropicFactory{}
	client, err := factory.New(
		core.ProviderInfo{ID: "anthropic", Type: "anthropic"},
		core.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL},
		"claude-3-5-sonnet-20241022",
	)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}

	req := core.LLMRequest{
		Model: core.ModelRef{Provider: "anthropic", Model: "claude-3-5-sonnet-20241022"},
		Messages: []core.Message{
			{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "what's the weather in Paris?"}}},
		},
	}

	var events []core.StreamEvent
	for ev, err := range client.Stream(context.Background(), req) {
		if err != nil {
			t.Fatalf("Stream: unexpected error: %v", err)
		}
		events = append(events, ev)
	}

	want := []core.StreamEvent{
		{Kind: core.StreamText, Text: "Hello"},
		{Kind: core.StreamText, Text: " world"},
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(events), events)
	}
	for i, w := range want {
		if events[i].Kind != w.Kind || events[i].Text != w.Text {
			t.Errorf("event %d: got %+v, want %+v", i, events[i], w)
		}
	}

	call := events[2]
	if call.Kind != core.StreamToolCall || call.Call == nil {
		t.Fatalf("event 2: got %+v, want StreamToolCall", call)
	}
	if call.Call.ID != "toolu_01abc" || call.Call.Name != "weather" || string(call.Call.Input) != `{"location":"Paris"}` {
		t.Errorf("tool call: got %+v, want {toolu_01abc weather {\"location\":\"Paris\"}}", call.Call)
	}

	finish := events[3]
	wantUsage := core.Usage{Input: 12, Output: 20, CacheRead: 4, CacheWrite: 3}
	if finish.Kind != core.StreamFinish || finish.FinishReason != "tool-calls" || finish.Usage != wantUsage {
		t.Errorf("finish event: got %+v, want FinishReason tool-calls with usage %+v", finish, wantUsage)
	}
}

func TestAnthropicFactory_429IsRetryable(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer srv.Close()

	factory := anthropicFactory{}
	client, err := factory.New(
		core.ProviderInfo{ID: "anthropic", Type: "anthropic"},
		core.ProviderConfig{APIKey: "test-key", BaseURL: srv.URL},
		"claude-3-5-sonnet-20241022",
	)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}

	req := core.LLMRequest{
		Model:    core.ModelRef{Provider: "anthropic", Model: "claude-3-5-sonnet-20241022"},
		Messages: []core.Message{{Role: core.RoleUser, Parts: []core.Part{{Kind: core.PartText, Text: "hi"}}}},
	}

	var gotErr error
	var n int
	for _, err := range client.Stream(context.Background(), req) {
		gotErr = err
		n++
	}
	if n != 1 {
		t.Fatalf("got %d stream events, want exactly 1 (the mapped error)", n)
	}

	var llmErr *core.LLMError
	if !errors.As(gotErr, &llmErr) {
		t.Fatalf("got %v (%T), want *core.LLMError", gotErr, gotErr)
	}
	if !llmErr.Retryable {
		t.Error("Retryable: got false, want true for a 429")
	}
	if llmErr.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter: got %v, want 2s", llmErr.RetryAfter)
	}

	// fantasy's anthropic provider always disables the underlying SDK's
	// retry loop (option.WithMaxRetries(0)), so jig's own retry layer
	// (Task 19) sees exactly one HTTP request per Stream call.
	if got := requests.Load(); got != 1 {
		t.Errorf("requests hitting the fixture server: got %d, want 1 (no SDK-internal retries)", got)
	}
}
