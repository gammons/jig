package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
)

// fakeTodoStore is a scripted TodoStore for todo tests.
type fakeTodoStore struct {
	err       error
	gotID     core.SessionID
	gotTodos  []core.Todo
	callCount int
}

func (f *fakeTodoStore) ReplaceTodos(_ context.Context, id core.SessionID, t []core.Todo) error {
	f.callCount++
	f.gotID = id
	f.gotTodos = t
	return f.err
}

// fakePublisher records every event published to it.
type fakePublisher struct {
	events []event.Event
}

func (f *fakePublisher) Publish(e event.Event) {
	f.events = append(f.events, e)
}

func TestTodo_ValidatesAndPublishes(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)

	todos := []map[string]any{
		{"content": "write tests", "status": "completed"},
		{"content": "implement", "status": "in_progress"},
		{"content": "review", "status": "pending"},
	}
	call := mustCall(t, "todo", map[string]any{"todos": todos})
	rc := rcFor(dir)
	res, err := tool.Run(context.Background(), rc, call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	if store.callCount != 1 {
		t.Fatalf("ReplaceTodos called %d times, want 1", store.callCount)
	}
	if store.gotID != rc.SessionID {
		t.Errorf("gotID = %q, want %q", store.gotID, rc.SessionID)
	}
	want := []core.Todo{
		{Content: "write tests", Status: "completed"},
		{Content: "implement", Status: "in_progress"},
		{Content: "review", Status: "pending"},
	}
	if len(store.gotTodos) != len(want) {
		t.Fatalf("gotTodos len = %d, want %d", len(store.gotTodos), len(want))
	}
	for i := range want {
		if store.gotTodos[i] != want[i] {
			t.Errorf("gotTodos[%d] = %+v, want %+v", i, store.gotTodos[i], want[i])
		}
	}

	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	tu, ok := pub.events[0].(event.TodosUpdated)
	if !ok {
		t.Fatalf("published event type = %T, want event.TodosUpdated", pub.events[0])
	}
	if tu.SessionID != rc.SessionID {
		t.Errorf("TodosUpdated.SessionID = %q, want %q", tu.SessionID, rc.SessionID)
	}
	if len(tu.Todos) != len(want) {
		t.Fatalf("TodosUpdated.Todos len = %d, want %d", len(tu.Todos), len(want))
	}

	if !strings.Contains(res.Output, "write tests") || !strings.Contains(res.Output, "implement") {
		t.Errorf("Output does not render the checklist: %q", res.Output)
	}
}

func TestTodo_RejectsTwoInProgress(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)

	todos := []map[string]any{
		{"content": "a", "status": "in_progress"},
		{"content": "b", "status": "in_progress"},
	}
	call := mustCall(t, "todo", map[string]any{"todos": todos})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("two in_progress: got IsError false, want true")
	}
	if res.Output != "only one todo may be in_progress" {
		t.Errorf("Output = %q, want %q", res.Output, "only one todo may be in_progress")
	}
	if store.callCount != 0 {
		t.Errorf("ReplaceTodos called %d times, want 0", store.callCount)
	}
	if len(pub.events) != 0 {
		t.Errorf("published %d events, want 0", len(pub.events))
	}
}

func TestTodo_RejectsInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)

	todos := []map[string]any{{"content": "a", "status": "bogus"}}
	call := mustCall(t, "todo", map[string]any{"todos": todos})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("invalid status: got IsError false, want true")
	}
	if !strings.Contains(res.Output, "bogus") {
		t.Errorf("Output = %q, want it to name the invalid status", res.Output)
	}
}

func TestTodo_RejectsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)

	todos := []map[string]any{{"content": "", "status": "pending"}}
	call := mustCall(t, "todo", map[string]any{"todos": todos})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("empty content: got IsError false, want true")
	}
}

func TestTodo_StoreError(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{err: errors.New("boom")}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)

	todos := []map[string]any{{"content": "a", "status": "pending"}}
	call := mustCall(t, "todo", map[string]any{"todos": todos})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("store error: got IsError false, want true")
	}
	if len(pub.events) != 0 {
		t.Errorf("published %d events, want 0 when the store fails", len(pub.events))
	}
}

func TestTodo_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	store := &fakeTodoStore{}
	pub := &fakePublisher{}
	tool := NewTodo(store, pub)
	call := mustCall(t, "todo", map[string]any{"todos": []map[string]any{}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}
