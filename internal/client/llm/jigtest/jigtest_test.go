//go:build jigtest

package jigtest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

func assistant(parts ...fantasy.MessagePart) fantasy.Message {
	return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}
}

func toolMsg(ids ...string) fantasy.Message {
	m := fantasy.Message{Role: fantasy.MessageRoleTool}
	for _, id := range ids {
		m.Content = append(m.Content, fantasy.ToolResultPart{ToolCallID: id, Output: fantasy.ToolResultOutputContentText{Text: "ok"}})
	}
	return m
}

func callPart(id, input string) fantasy.ToolCallPart {
	return fantasy.ToolCallPart{ToolCallID: id, ToolName: "bash", Input: input}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		prompt  []fantasy.Message
		wantErr string
	}{
		{"paired", []fantasy.Message{assistant(callPart("c1", `{}`)), toolMsg("c1")}, ""},
		{"text only", []fantasy.Message{assistant(fantasy.TextPart{Text: "hi"})}, ""},
		{"unpaired", []fantasy.Message{assistant(callPart("c1", `{}`), callPart("c2", `{}`)), toolMsg("c1")}, "jigtest: unpaired tool call c2"},
		{"result before call", []fantasy.Message{toolMsg("c1"), assistant(callPart("c1", `{}`))}, "jigtest: unpaired tool call c1"},
		{"empty assistant", []fantasy.Message{assistant()}, "jigtest: empty assistant message"},
		{"array input", []fantasy.Message{assistant(callPart("c1", `[1]`)), toolMsg("c1")}, "not a JSON object"},
		{"null input", []fantasy.Message{assistant(callPart("c1", `null`)), toolMsg("c1")}, "not a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.prompt)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("validate: %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("validate: %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// newLLM writes s to a temp file and returns a model from a new factory
// over it, plus the factory and the script path.
func newLLM(t *testing.T, s Script, model string) (core.LLM, *factory, string) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f := Factory().(*factory)
	l, err := f.New(core.ProviderInfo{}, cfgFor(path), model)
	if err != nil {
		t.Fatal(err)
	}
	return l, f, path
}

func cfgFor(path string) core.ProviderConfig {
	return core.ProviderConfig{Options: map[string]any{"script": path}}
}

// collect drains one Stream, returning its events and final error.
func collect(ctx context.Context, l core.LLM, req core.LLMRequest) ([]core.StreamEvent, error) {
	var events []core.StreamEvent
	for ev, err := range l.Stream(ctx, req) {
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func TestStream_ReplaysTextCallsAndFinish(t *testing.T) {
	l, _, _ := newLLM(t, Script{Models: map[string][]Turn{"m1": {{
		Text:  "hi",
		Calls: []Call{{ID: "c1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)}},
	}}}}, "m1")

	events, err := collect(context.Background(), l, core.LLMRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v, want text, tool_call, finish", events)
	}
	if events[0].Kind != core.StreamText || events[0].Text != "hi" {
		t.Errorf("events[0] = %+v, want text hi", events[0])
	}
	if c := events[1].Call; events[1].Kind != core.StreamToolCall || c == nil || c.ID != "c1" || c.Name != "bash" || string(c.Input) != `{"command":"ls"}` {
		t.Errorf("events[1] = %+v, want the bash call", events[1])
	}
	if events[2].Kind != core.StreamFinish || events[2].FinishReason != "tool_calls" || events[2].Usage != usage() {
		t.Errorf("events[2] = %+v, want a tool_calls finish with usage", events[2])
	}
}

func TestStream_QueueSharedPerPathUntilExhausted(t *testing.T) {
	_, f, path := newLLM(t, Script{Models: map[string][]Turn{"m1": {{Text: "one"}, {Text: "two"}}}}, "m1")
	for _, want := range []string{"one", "two"} {
		l, err := f.New(core.ProviderInfo{}, cfgFor(path), "m1") // a fresh LLM per step, as Source.For builds
		if err != nil {
			t.Fatal(err)
		}
		events, err := collect(context.Background(), l, core.LLMRequest{})
		if err != nil || len(events) == 0 || events[0].Text != want {
			t.Fatalf("events = %+v, err = %v; want text %q", events, err, want)
		}
	}
	l, _ := f.New(core.ProviderInfo{}, cfgFor(path), "m1")
	_, err := collect(context.Background(), l, core.LLMRequest{})
	assertLLMError(t, err, "jigtest: no turn left for model m1", false)
}

func TestStream_ExpectSystemContains(t *testing.T) {
	turn := Turn{Text: "ok", ExpectSystemContains: []string{"alpha", "beta\ngamma"}}
	l, _, _ := newLLM(t, Script{Models: map[string][]Turn{"m1": {turn, turn}}}, "m1")

	if _, err := collect(context.Background(), l, core.LLMRequest{System: []string{"x alpha", "beta", "gamma"}}); err != nil {
		t.Fatalf("all present across joined system strings: %v", err)
	}
	_, err := collect(context.Background(), l, core.LLMRequest{System: []string{"alpha"}})
	assertLLMError(t, err, `"beta\ngamma"`, false)
}

func TestStream_ScriptedErr(t *testing.T) {
	l, _, _ := newLLM(t, Script{Models: map[string][]Turn{"m1": {
		{Err: "overloaded", Retryable: true},
		{Err: "bad request"},
	}}}, "m1")
	_, err := collect(context.Background(), l, core.LLMRequest{})
	assertLLMError(t, err, "overloaded", true)
	_, err = collect(context.Background(), l, core.LLMRequest{})
	assertLLMError(t, err, "bad request", false)
}

func TestStream_HangBlocksUntilCancelled(t *testing.T) {
	l, _, _ := newLLM(t, Script{Models: map[string][]Turn{"m1": {{Text: "working", Hang: true}}}}, "m1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []core.StreamEvent
	var final error
	for ev, err := range l.Stream(ctx, core.LLMRequest{}) {
		if err != nil {
			final = err
			break
		}
		got = append(got, ev)
		cancel() // the text arrived; the stream now blocks until this
	}
	if len(got) != 1 || got[0].Text != "working" {
		t.Errorf("events = %+v, want only the text", got)
	}
	if !errors.Is(final, context.Canceled) {
		t.Errorf("final error = %v, want context.Canceled", final)
	}
}

func TestNew_MissingScriptOption(t *testing.T) {
	if _, err := Factory().New(core.ProviderInfo{}, core.ProviderConfig{}, "m1"); err == nil {
		t.Fatal("New with no options.script: want error")
	}
}

func assertLLMError(t *testing.T, err error, contains string, retryable bool) {
	t.Helper()
	var le *core.LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v (%T), want *core.LLMError", err, err)
	}
	if le.Retryable != retryable || !strings.Contains(le.Error(), contains) {
		t.Fatalf("err = %q retryable=%v, want containing %q retryable=%v", le.Error(), le.Retryable, contains, retryable)
	}
}
