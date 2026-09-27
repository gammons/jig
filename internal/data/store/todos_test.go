package store

import (
	"context"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestTodos_Replace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	first := []core.Todo{
		{Content: "write tests", Status: "in_progress"},
		{Content: "implement", Status: "pending"},
	}
	if err := s.ReplaceTodos(ctx, "ses_1", first); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}

	got, err := s.ListTodos(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(got) != len(first) {
		t.Fatalf("ListTodos = %d items, want %d", len(got), len(first))
	}
	for i, want := range first {
		if got[i] != want {
			t.Errorf("todo %d = %+v, want %+v", i, got[i], want)
		}
	}

	second := []core.Todo{
		{Content: "ship", Status: "completed"},
	}
	if err := s.ReplaceTodos(ctx, "ses_1", second); err != nil {
		t.Fatalf("ReplaceTodos (2nd): %v", err)
	}

	got2, err := s.ListTodos(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListTodos (2nd): %v", err)
	}
	if len(got2) != 1 || got2[0] != second[0] {
		t.Errorf("ListTodos (2nd) = %+v, want %+v", got2, second)
	}
}

func TestTodos_ReplaceEmptyClearsAll(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "ses_1")

	if err := s.ReplaceTodos(ctx, "ses_1", []core.Todo{{Content: "x", Status: "pending"}}); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}
	if err := s.ReplaceTodos(ctx, "ses_1", nil); err != nil {
		t.Fatalf("ReplaceTodos (empty): %v", err)
	}

	got, err := s.ListTodos(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListTodos after clearing = %+v, want empty", got)
	}
}
