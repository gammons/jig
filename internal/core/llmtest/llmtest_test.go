package llmtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

func TestClient_ReplaysTurnsInOrder(t *testing.T) {
	c := New(Text("hello"), Calls(Call("c1", "bash", `{"cmd":"ls"}`)))

	// First turn.
	var events []core.StreamEvent
	for ev, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		if err != nil {
			t.Fatalf("turn 1: unexpected error: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("turn 1: got %d events, want 2", len(events))
	}
	if events[0].Kind != core.StreamText || events[0].Text != "hello" {
		t.Fatalf("turn 1 event 0: got %+v, want StreamText %q", events[0], "hello")
	}
	if events[1].Kind != core.StreamFinish || events[1].FinishReason != "stop" {
		t.Fatalf("turn 1 event 1: got %+v, want StreamFinish stop", events[1])
	}
	if events[1].Usage != (core.Usage{Input: 10, Output: 5}) {
		t.Fatalf("turn 1 finish usage: got %+v, want {10 5 0 0}", events[1].Usage)
	}

	// Second turn.
	events = nil
	for ev, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		if err != nil {
			t.Fatalf("turn 2: unexpected error: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("turn 2: got %d events, want 2", len(events))
	}
	if events[0].Kind != core.StreamToolCall || events[0].Call == nil || events[0].Call.ID != "c1" {
		t.Fatalf("turn 2 event 0: got %+v, want StreamToolCall c1", events[0])
	}
	if events[1].Kind != core.StreamFinish || events[1].FinishReason != "tool_calls" {
		t.Fatalf("turn 2 event 1: got %+v, want StreamFinish tool_calls", events[1])
	}
}

func TestClient_RecordsRequests(t *testing.T) {
	c := New(Text("a"), Text("b"))

	req1 := core.LLMRequest{Model: core.ModelRef{Model: "m1"}}
	req2 := core.LLMRequest{Model: core.ModelRef{Model: "m2"}}

	for range c.Stream(context.Background(), req1) {
	}
	for range c.Stream(context.Background(), req2) {
	}

	got := c.Requests()
	if len(got) != 2 {
		t.Fatalf("got %d requests, want 2", len(got))
	}
	if got[0].Model.Model != "m1" || got[1].Model.Model != "m2" {
		t.Fatalf("got %+v, want models m1, m2 in order", got)
	}

	// Mutating the returned slice must not affect recorded state.
	got[0].Model.Model = "mutated"
	again := c.Requests()
	if again[0].Model.Model != "m1" {
		t.Fatalf("mutating Requests() result leaked into recorded state: got %q, want %q", again[0].Model.Model, "m1")
	}
}

func TestClient_HangRespectsCancel(t *testing.T) {
	c := New(Turn{Hang: true})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		var lastErr error
		for _, err := range c.Stream(ctx, core.LLMRequest{}) {
			lastErr = err
		}
		errCh <- lastErr
	}()

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got err %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hang to respect cancellation")
	}
}

func TestClient_ExhaustedTurnsError(t *testing.T) {
	c := New(Text("only turn"))

	for range c.Stream(context.Background(), core.LLMRequest{}) {
	}

	var gotErr error
	for _, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("got nil error, want exhausted-turns error")
	}
	want := "llmtest: no scripted turn for request 2"
	if gotErr.Error() != want {
		t.Fatalf("got error %q, want %q", gotErr.Error(), want)
	}
}

func TestClient_StopsWhenConsumerBreaks(t *testing.T) {
	c := New(Text("hello world"))

	var n int
	for range c.Stream(context.Background(), core.LLMRequest{}) {
		n++
		break
	}
	if n != 1 {
		t.Fatalf("got %d events before break, want 1", n)
	}
}

func TestClient_HandBuiltTurnZeroUsageGetsDefault(t *testing.T) {
	c := New(Turn{Events: []core.StreamEvent{
		{Kind: core.StreamFinish, FinishReason: "stop"},
	}})

	var got core.StreamEvent
	for ev, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = ev
	}
	if got.Usage != (core.Usage{Input: 10, Output: 5}) {
		t.Fatalf("got usage %+v, want {10 5 0 0}", got.Usage)
	}
}

func TestClient_HandBuiltTurnNonZeroUsagePreserved(t *testing.T) {
	custom := core.Usage{Input: 100, Output: 50, CacheRead: 1}
	c := New(Turn{Events: []core.StreamEvent{
		{Kind: core.StreamFinish, FinishReason: "stop", Usage: custom},
	}})

	var got core.StreamEvent
	for ev, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = ev
	}
	if got.Usage != custom {
		t.Fatalf("got usage %+v, want %+v", got.Usage, custom)
	}
}

func TestClient_TurnErrYieldedAfterEvents(t *testing.T) {
	wantErr := errors.New("boom")
	c := New(Turn{Events: []core.StreamEvent{{Kind: core.StreamText, Text: "partial"}}, Err: wantErr})

	var events []core.StreamEvent
	var gotErr error
	for ev, err := range c.Stream(context.Background(), core.LLMRequest{}) {
		if err != nil {
			gotErr = err
			break
		}
		events = append(events, ev)
	}
	if len(events) != 1 || events[0].Text != "partial" {
		t.Fatalf("got events %+v, want one partial text event", events)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("got err %v, want %v", gotErr, wantErr)
	}
}
