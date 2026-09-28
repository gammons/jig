// Package llmtest provides a scripted fake core.LLM for service tests: a
// Client replays a fixed sequence of Turns and records every LLMRequest it
// received.
package llmtest

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/gammons/jig/internal/core"
)

// defaultUsage is the Usage every StreamFinish carries unless the turn sets
// its own.
func defaultUsage() core.Usage {
	return core.Usage{Input: 10, Output: 5}
}

// Turn scripts one response to a Stream call.
type Turn struct {
	Events []core.StreamEvent
	Err    error // yielded after Events
	Hang   bool  // after Events, block until ctx is done, then yield ctx.Err()
	// Gate, when set, blocks Stream before it yields anything, until Gate
	// is closed or ctx is done. It lets a test start a call, act while it
	// is blocked, then release it deterministically.
	Gate <-chan struct{}
}

// Text returns a Turn that streams a single StreamText event followed by a
// StreamFinish with FinishReason "stop".
func Text(s string) Turn {
	return Turn{Events: []core.StreamEvent{
		{Kind: core.StreamText, Text: s},
		{Kind: core.StreamFinish, FinishReason: "stop"},
	}}
}

// Calls returns a Turn that streams a StreamToolCall event per call,
// followed by a StreamFinish with FinishReason "tool_calls".
func Calls(calls ...core.ToolCall) Turn {
	events := make([]core.StreamEvent, 0, len(calls)+1)
	for i := range calls {
		call := calls[i]
		events = append(events, core.StreamEvent{Kind: core.StreamToolCall, Call: &call})
	}
	events = append(events, core.StreamEvent{Kind: core.StreamFinish, FinishReason: "tool_calls"})
	return Turn{Events: events}
}

// Call builds a core.ToolCall with the given id, name, and raw JSON input.
func Call(id, name, inputJSON string) core.ToolCall {
	return core.ToolCall{ID: id, Name: name, Input: []byte(inputJSON)}
}

// Client is a scripted fake core.LLM. It replays turns in order and records
// every request it receives. It is safe for concurrent use.
type Client struct {
	mu       sync.Mutex
	turns    []Turn
	requests []core.LLMRequest
}

// New returns a Client that replays turns in order, one per call to Stream.
func New(turns ...Turn) *Client {
	return &Client{turns: turns}
}

// Requests returns every LLMRequest passed to Stream, in call order. The
// returned slice is a copy: mutating it does not affect recorded state.
func (c *Client) Requests() []core.LLMRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]core.LLMRequest, len(c.requests))
	copy(out, c.requests)
	return out
}

// Stream records req and replays the next scripted Turn. Running out of
// turns yields an error of the form "llmtest: no scripted turn for request
// N", where N is the 1-based request number.
func (c *Client) Stream(ctx context.Context, req core.LLMRequest) iter.Seq2[core.StreamEvent, error] {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	n := len(c.requests)
	var turn Turn
	var ok bool
	if n-1 < len(c.turns) {
		turn, ok = c.turns[n-1], true
	}
	c.mu.Unlock()

	return func(yield func(core.StreamEvent, error) bool) {
		if !ok {
			yield(core.StreamEvent{}, fmt.Errorf("llmtest: no scripted turn for request %d", n))
			return
		}
		streamTurn(ctx, turn, yield)
	}
}

// streamTurn waits on turn.Gate (if set), then yields turn's events,
// applying the default Usage to any zero-Usage StreamFinish, then its Err
// (if any), then, if Hang is set, blocks until ctx is done and yields
// ctx.Err(). It checks ctx before each event and stops promptly if yield
// returns false.
func streamTurn(ctx context.Context, turn Turn, yield func(core.StreamEvent, error) bool) {
	if turn.Gate != nil {
		select {
		case <-turn.Gate:
		case <-ctx.Done():
			yield(core.StreamEvent{}, ctx.Err())
			return
		}
	}
	for _, ev := range turn.Events {
		if err := ctx.Err(); err != nil {
			yield(core.StreamEvent{}, err)
			return
		}
		if ev.Kind == core.StreamFinish && ev.Usage == (core.Usage{}) {
			ev.Usage = defaultUsage()
		}
		if !yield(ev, nil) {
			return
		}
	}
	if turn.Err != nil {
		yield(core.StreamEvent{}, turn.Err)
		return
	}
	if turn.Hang {
		<-ctx.Done()
		yield(core.StreamEvent{}, ctx.Err())
	}
}
