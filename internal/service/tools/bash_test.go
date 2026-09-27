package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core/ext"
)

// fakeShell is a scripted Shell for bash tests. It records the ShellSpec
// it was called with and, when spec.SpillPath is set, writes fullOutput
// to it, mimicking what a real Shell does.
type fakeShell struct {
	result     ShellResult
	err        error
	fullOutput []byte
	gotSpec    ShellSpec
}

func (f *fakeShell) Run(_ context.Context, spec ShellSpec) (ShellResult, error) {
	f.gotSpec = spec
	if spec.SpillPath != "" && f.fullOutput != nil {
		if err := os.WriteFile(spec.SpillPath, f.fullOutput, 0o600); err != nil {
			return ShellResult{}, err
		}
	}
	return f.result, f.err
}

func TestBash_TruncatesAndSavesFullOutput(t *testing.T) {
	dir := t.TempDir()
	full := strings.Repeat("x", 40*1024)
	tail := full[len(full)-30*1024:]

	sh := &fakeShell{
		result: ShellResult{
			Output:     []byte(tail),
			ExitCode:   0,
			Truncated:  true,
			TotalBytes: int64(len(full)),
		},
		fullOutput: []byte(full),
	}

	tool := NewBash(sh, dir)
	call := mustCall(t, "bash", map[string]any{"command": "produce-big-output"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	wantPath := filepath.Join(dir, fmt.Sprintf("jig-bash-%s.log", call.ID))
	if sh.gotSpec.SpillPath != wantPath {
		t.Errorf("SpillPath = %q, want %q", sh.gotSpec.SpillPath, wantPath)
	}
	if sh.gotSpec.TailBytes != 30*1024 {
		t.Errorf("TailBytes = %d, want %d", sh.gotSpec.TailBytes, 30*1024)
	}

	wantPrefix := fmt.Sprintf("[output truncated; full output: %s]\n", wantPath)
	if !strings.HasPrefix(res.Output, wantPrefix) {
		t.Errorf("Output does not start with truncation notice: %q", res.Output[:min(len(res.Output), 100)])
	}
	if !strings.HasSuffix(res.Output, tail) {
		t.Errorf("Output does not end with the tail")
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("spill file missing: %v", err)
	}
	if string(data) != full {
		t.Errorf("spill file content mismatch: got %d bytes, want %d", len(data), len(full))
	}
}

func TestBash_NonZeroExitNotError(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{
		result: ShellResult{Output: []byte("boom"), ExitCode: 2, TotalBytes: 4},
	}

	tool := NewBash(sh, dir)
	call := mustCall(t, "bash", map[string]any{"command": "false"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, want false for a non-zero exit")
	}
	want := "boom\n[exit code 2]"
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}

	// Spill file should be removed since output was not truncated.
	spillPath := filepath.Join(dir, fmt.Sprintf("jig-bash-%s.log", call.ID))
	if _, err := os.Stat(spillPath); !os.IsNotExist(err) {
		t.Errorf("spill file should have been removed, stat err = %v", err)
	}
}

func TestBash_TimeoutCappedAndError(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{
		result: ShellResult{Output: []byte("partial"), TimedOut: true, TotalBytes: 7},
	}

	tool := NewBash(sh, dir)
	call := mustCall(t, "bash", map[string]any{"command": "sleep 1000", "timeout_ms": 700000})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for a timeout")
	}
	if sh.gotSpec.Timeout.Milliseconds() != 600000 {
		t.Errorf("Timeout = %v, want 600000ms (capped)", sh.gotSpec.Timeout)
	}
	if !strings.HasSuffix(res.Output, "\n[timed out after 600s]") {
		t.Errorf("Output = %q, want suffix \"\\n[timed out after 600s]\"", res.Output)
	}
}

func TestBash_DefaultAndInvalidTimeout(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{result: ShellResult{Output: []byte("ok")}}
	tool := NewBash(sh, dir)

	// timeout_ms omitted -> default 120000ms.
	call := mustCall(t, "bash", map[string]any{"command": "echo hi"})
	if _, err := tool.Run(context.Background(), rcFor(dir), call); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sh.gotSpec.Timeout.Milliseconds() != 120000 {
		t.Errorf("default Timeout = %v, want 120000ms", sh.gotSpec.Timeout)
	}

	// timeout_ms <= 0 -> default 120000ms.
	call = mustCall(t, "bash", map[string]any{"command": "echo hi", "timeout_ms": -5})
	if _, err := tool.Run(context.Background(), rcFor(dir), call); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sh.gotSpec.Timeout.Milliseconds() != 120000 {
		t.Errorf("negative timeout_ms Timeout = %v, want 120000ms", sh.gotSpec.Timeout)
	}
}

func TestBash_SubjectIsCommand(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{}
	tool := NewBash(sh, dir)
	subjecter, ok := tool.(ext.Subjecter)
	if !ok {
		t.Fatal("bash tool does not implement ext.Subjecter")
	}

	call := mustCall(t, "bash", map[string]any{"command": "echo hi"})
	if got := subjecter.Subject(rcFor(dir), call.Input); got != "echo hi" {
		t.Errorf("Subject = %q, want %q", got, "echo hi")
	}

	if got := subjecter.Subject(rcFor(dir), []byte("not json")); got != "" {
		t.Errorf("Subject on unparsable input = %q, want \"\"", got)
	}
}

func TestBash_MissingCommand(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{}
	tool := NewBash(sh, dir)

	call := mustCall(t, "bash", map[string]any{})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("missing command: got IsError false, want true")
	}
}

func TestBash_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	sh := &fakeShell{}
	tool := NewBash(sh, dir)
	call := mustCall(t, "bash", map[string]any{"command": "echo hi"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}
