package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/jig/internal/core"
)

// seqIDs is a deterministic IDSource: prefix_1, prefix_2, ...
type seqIDs struct{ n int }

func (s *seqIDs) Next(prefix string) string {
	s.n++
	return fmt.Sprintf("%s_%d", prefix, s.n)
}

// spillingShell writes to spec.SpillPath (as a real Shell does), then
// returns result/err, optionally cancelling the caller's ctx first.
type spillingShell struct {
	result  ShellResult
	err     error
	cancel  context.CancelFunc
	gotSpec ShellSpec
}

func (s *spillingShell) Run(_ context.Context, spec ShellSpec) (ShellResult, error) {
	s.gotSpec = spec
	if err := os.WriteFile(spec.SpillPath, []byte("partial output"), 0o600); err != nil {
		return ShellResult{}, err
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.result, s.err
}

func TestBash_SpillNameIgnoresCallID(t *testing.T) {
	dir := t.TempDir()
	sh := &spillingShell{result: ShellResult{Output: []byte("x"), Truncated: true}}
	tool := NewBash(sh, dir, &seqIDs{})
	call := core.ToolCall{ID: "../../escape", Name: "bash", Input: []byte(`{"command":"echo"}`)}
	if _, err := tool.Run(context.Background(), rcFor(dir), call); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := filepath.Join(dir, "jig-bash-out_1.log"); sh.gotSpec.SpillPath != want {
		t.Errorf("SpillPath = %q, want %q", sh.gotSpec.SpillPath, want)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.log")); !os.IsNotExist(err) {
		t.Errorf("a file escaped the spill dir: stat err = %v", err)
	}
}

func TestBash_SpillRemovedWhenNotTruncated(t *testing.T) {
	cases := map[string]func(cancel context.CancelFunc) *spillingShell{
		"shell error": func(context.CancelFunc) *spillingShell {
			return &spillingShell{err: errors.New("boom")}
		},
		"cancelled": func(cancel context.CancelFunc) *spillingShell {
			return &spillingShell{err: context.Canceled, cancel: cancel}
		},
		"timeout": func(context.CancelFunc) *spillingShell {
			return &spillingShell{result: ShellResult{Output: []byte("p"), TimedOut: true}}
		},
		"success": func(context.CancelFunc) *spillingShell {
			return &spillingShell{result: ShellResult{Output: []byte("ok")}}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sh := mk(cancel)
			_, _ = NewBash(sh, dir, &seqIDs{}).Run(ctx, rcFor(dir), core.ToolCall{ID: "c1", Name: "bash", Input: []byte(`{"command":"echo"}`)})
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("spill dir holds %v, want it empty", entries)
			}
		})
	}
}
