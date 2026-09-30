//go:build jigtest

// Package jigtest is a scripted provider for end-to-end tests: a provider
// of type "jigtest" replays the turns of a JSON Script instead of calling
// a real model, after checking that the request it was sent would be
// acceptable to a real provider. It is compiled only with the jigtest
// build tag.
package jigtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"slices"
	"strings"
	"sync"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/client/llm"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// Script maps a model ID to its queue of turns; each model's queue is
// consumed in order, one turn per Stream.
type Script struct{ Models map[string][]Turn }

// Call is one scripted tool call.
type Call = struct {
	ID, Name string
	Input    json.RawMessage
}

// Turn is one scripted model response.
type Turn struct {
	Text      string
	Calls     []Call
	Hang      bool // emit Text, then block until ctx is done
	Err       string
	Retryable bool
	// ExpectSystemContains fails the turn with a non-retryable error if
	// any entry is missing from the request's system prompt.
	ExpectSystemContains []string
	// ExpectMedia, if set, is the exact number of media parts (tool-result
	// media outputs plus file parts) the converted prompt must hold.
	ExpectMedia *int
	// ExpectPromptContains lists substrings of the converted prompt's
	// concatenated text: text parts plus tool-result text and error text.
	ExpectPromptContains []string
	// ExpectEffort, if set, is the exact effort the request must carry.
	ExpectEffort *string
}

// usage is the token usage every finished turn reports.
func usage() core.Usage { return core.Usage{Input: 10, Output: 5} }

// Factory returns the "jigtest" ext.ProviderFactory. The script path comes
// from the provider config's options.script; every LLM the factory builds
// for the same path shares one set of queues.
func Factory() ext.ProviderFactory {
	return &factory{scripts: make(map[string]*Script)}
}

type factory struct {
	mu      sync.Mutex
	scripts map[string]*Script // script path -> remaining queues
}

func (*factory) Type() string { return "jigtest" }

func (f *factory) New(_ core.ProviderInfo, cfg core.ProviderConfig, model string) (core.LLM, error) {
	path, _ := cfg.Options["script"].(string)
	if path == "" {
		return nil, errors.New("jigtest: providers.<id>.options.script is not set")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.scripts[path]; !ok {
		s, err := load(path)
		if err != nil {
			return nil, err
		}
		f.scripts[path] = s
	}
	return &client{f: f, path: path, model: model}, nil
}

func load(path string) (*Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("jigtest: %w", err)
	}
	var s Script
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("jigtest: parsing %s: %w", path, err)
	}
	return &s, nil
}

// next pops model's next turn from the script at path.
func (f *factory) next(path, model string) (Turn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.scripts[path]
	q := s.Models[model]
	if len(q) == 0 {
		return Turn{}, fmt.Errorf("jigtest: no turn left for model %s", model)
	}
	s.Models[model] = q[1:]
	return q[0], nil
}

type client struct {
	f     *factory
	path  string
	model string
}

// Stream validates req as a real provider would see it, then replays the
// model's next turn.
func (c *client) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	return func(yield func(core.StreamEvent, error) bool) {
		prompt := llm.ToFantasy(req).Prompt
		if err := validate(prompt); err != nil {
			yield(core.StreamEvent{}, fatal(err))
			return
		}
		turn, err := c.f.next(c.path, c.model)
		if err != nil {
			yield(core.StreamEvent{}, fatal(err))
			return
		}
		if err := checkSystem(req.System, turn.ExpectSystemContains); err != nil {
			yield(core.StreamEvent{}, fatal(err))
			return
		}
		if err := checkPrompt(prompt, turn); err != nil {
			yield(core.StreamEvent{}, fatal(err))
			return
		}
		if turn.ExpectEffort != nil && string(req.Effort) != *turn.ExpectEffort {
			yield(core.StreamEvent{}, fatal(fmt.Errorf("jigtest: effort %q, want %q", req.Effort, *turn.ExpectEffort)))
			return
		}
		replay(ctx, turn, yield)
	}
}

func fatal(err error) error { return &core.LLMError{Err: err} }

// validate checks the invariants real providers enforce: no empty
// assistant message, object tool inputs, and every tool call answered by
// a result in a later tool message.
func validate(prompt []fantasy.Message) error {
	var pending []string
	answered := map[string]bool{}
	for _, m := range prompt {
		switch m.Role {
		case fantasy.MessageRoleAssistant:
			ids, err := assistantCalls(m)
			if err != nil {
				return err
			}
			pending = append(pending, ids...)
		case fantasy.MessageRoleTool:
			for _, p := range m.Content {
				if r, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](p); ok && slices.Contains(pending, r.ToolCallID) {
					answered[r.ToolCallID] = true
				}
			}
		}
	}
	for _, id := range pending {
		if !answered[id] {
			return fmt.Errorf("jigtest: unpaired tool call %s", id)
		}
	}
	return nil
}

// assistantCalls returns the IDs of m's tool calls, failing if m is empty
// or a call's input is not a JSON object.
func assistantCalls(m fantasy.Message) ([]string, error) {
	if len(m.Content) == 0 {
		return nil, errors.New("jigtest: empty assistant message")
	}
	var ids []string
	for _, p := range m.Content {
		tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](p)
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.Input), &obj); err != nil || obj == nil {
			return nil, fmt.Errorf("jigtest: tool call %s input is not a JSON object: %q", tc.ToolCallID, tc.Input)
		}
		ids = append(ids, tc.ToolCallID)
	}
	return ids, nil
}

// checkSystem fails if any of want is missing from the system prompt.
func checkSystem(system, want []string) error {
	joined := strings.Join(system, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			return fmt.Errorf("jigtest: system prompt does not contain %q", w)
		}
	}
	return nil
}

// checkPrompt fails if prompt's media count differs from
// turn.ExpectMedia (when set) or its text lacks an ExpectPromptContains
// entry.
func checkPrompt(prompt []fantasy.Message, turn Turn) error {
	media, text := promptContent(prompt)
	if turn.ExpectMedia != nil && media != *turn.ExpectMedia {
		return fmt.Errorf("jigtest: prompt has %d media, want %d", media, *turn.ExpectMedia)
	}
	for _, w := range turn.ExpectPromptContains {
		if !strings.Contains(text, w) {
			return fmt.Errorf("jigtest: prompt does not contain %q", w)
		}
	}
	return nil
}

// promptContent counts prompt's media parts and joins its text.
func promptContent(prompt []fantasy.Message) (int, string) {
	var media int
	var text strings.Builder
	for _, m := range prompt {
		for _, p := range m.Content {
			if _, ok := fantasy.AsMessagePart[fantasy.FilePart](p); ok {
				media++
			}
			if t, ok := fantasy.AsMessagePart[fantasy.TextPart](p); ok {
				text.WriteString(t.Text + "\n")
			}
			if r, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](p); ok {
				n, s := resultContent(r.Output)
				media += n
				text.WriteString(s + "\n")
			}
		}
	}
	return media, text.String()
}

// resultContent returns a tool result's media count and text.
func resultContent(out fantasy.ToolResultOutputContent) (int, string) {
	if m, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](out); ok {
		return 1, m.Text
	}
	if t, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](out); ok {
		return 0, t.Text
	}
	if e, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](out); ok && e.Error != nil {
		return 0, e.Error.Error()
	}
	return 0, ""
}

// replay streams turn: its text, then its error, hang, or tool calls and
// a finish event.
func replay(ctx context.Context, turn Turn, yield func(core.StreamEvent, error) bool) {
	events := []core.StreamEvent{}
	if turn.Text != "" {
		events = append(events, core.StreamEvent{Kind: core.StreamText, Text: turn.Text})
	}
	if !emit(ctx, events, yield) {
		return
	}
	switch {
	case turn.Err != "":
		yield(core.StreamEvent{}, &core.LLMError{Retryable: turn.Retryable, Err: errors.New(turn.Err)})
	case turn.Hang:
		<-ctx.Done()
		yield(core.StreamEvent{}, ctx.Err())
	default:
		emit(ctx, finishEvents(turn.Calls), yield)
	}
}

// finishEvents is one tool_call event per call, then a finish event.
func finishEvents(calls []Call) []core.StreamEvent {
	events := make([]core.StreamEvent, 0, len(calls)+1)
	reason := "stop"
	for _, c := range calls {
		events = append(events, core.StreamEvent{
			Kind: core.StreamToolCall,
			Call: &core.ToolCall{ID: c.ID, Name: c.Name, Input: c.Input},
		})
		reason = "tool_calls"
	}
	return append(events, core.StreamEvent{Kind: core.StreamFinish, Usage: usage(), FinishReason: reason})
}

// emit yields events in order, stopping with ctx's error once ctx is done.
// It reports whether the consumer wants more.
func emit(ctx context.Context, events []core.StreamEvent, yield func(core.StreamEvent, error) bool) bool {
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			yield(core.StreamEvent{}, err)
			return false
		}
		if !yield(ev, nil) {
			return false
		}
	}
	return true
}
