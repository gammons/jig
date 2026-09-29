package core_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestWithLogAttrs_ReplacesSameKeyKeepsOthers(t *testing.T) {
	ctx := core.WithLogAttrs(context.Background(), slog.String("root", "r"), slog.String("session", "p"), slog.Int("depth", 0))
	ctx = core.WithLogAttrs(ctx, slog.String("session", "c"), slog.Int("depth", 1))
	got := fmt.Sprint(core.LogAttrs(ctx))
	if got != "[root=r session=c depth=1]" {
		t.Errorf("LogAttrs = %s", got)
	}
}

func TestLogAttrs_BareContextIsEmpty(t *testing.T) {
	if a := core.LogAttrs(context.Background()); len(a) != 0 {
		t.Errorf("LogAttrs = %v, want empty", a)
	}
}

func TestLogAttrs_ParentUnchanged(t *testing.T) {
	parent := core.WithLogAttrs(context.Background(), slog.String("session", "p"))
	_ = core.WithLogAttrs(parent, slog.String("session", "c"))
	if got := fmt.Sprint(core.LogAttrs(parent)); got != "[session=p]" {
		t.Errorf("parent attrs = %s", got)
	}
}

func TestLogAttrs_ReturnsCopy(t *testing.T) {
	ctx := core.WithLogAttrs(context.Background(), slog.String("session", "p"))
	core.LogAttrs(ctx)[0] = slog.String("session", "x")
	if got := fmt.Sprint(core.LogAttrs(ctx)); got != "[session=p]" {
		t.Errorf("attrs = %s", got)
	}
}

func TestLogArgs_MatchesAttrs(t *testing.T) {
	ctx := core.WithLogAttrs(context.Background(), slog.String("root", "r"), slog.Int("depth", 2))
	args := core.LogArgs(ctx)
	if len(args) != 2 {
		t.Fatalf("LogArgs len = %d, want 2", len(args))
	}
	if a, ok := args[1].(slog.Attr); !ok || a.Key != "depth" {
		t.Errorf("args[1] = %#v", args[1])
	}
	if core.LogArgs(context.Background()) != nil {
		t.Errorf("bare LogArgs should be nil")
	}
}
