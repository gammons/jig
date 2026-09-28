package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

func mustCall(t *testing.T, name string, input any) core.ToolCall {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return core.ToolCall{ID: "c1", Name: name, Input: raw}
}

func rcFor(dir string) ext.RunContext {
	return ext.RunContext{SessionID: core.SessionID("s1"), WorkDir: dir}
}

func TestRead_LineNumbersOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "one\ntwo\nthree\nfour\nfive\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path, "offset": 2, "limit": 2})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	want := "2: two\n3: three\n(more lines; continue with offset 4)"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_LongLineTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	long := strings.Repeat("x", 2500)
	if err := os.WriteFile(path, []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	want := "1: " + strings.Repeat("x", 2000) + "…"
	if res.Output != want {
		t.Errorf("got line of %d chars, want truncated to 2000 + ellipsis", len(res.Output))
	}
}

func TestRead_BinaryRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	data := append([]byte("hello"), 0x00, 'w', 'o', 'r', 'l', 'd')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a binary file")
	}
	want := path + " appears to be binary"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_Directory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": dir})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	want := "a.txt\nb.txt\nsub/"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_DirectoryDoesNotMarkRead(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker()
	tool := NewRead(OSFS(), tr, nil)
	call := mustCall(t, "read", map[string]any{"path": dir})
	if _, err := tool.Run(context.Background(), rcFor(dir), call); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := tr.CheckWritable(core.SessionID("s1"), dir, nil); err != nil {
		t.Fatalf("CheckWritable(nil): %v", err)
	}
}

func TestRead_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if res.Output != "(empty file)" {
		t.Errorf("got %q, want %q", res.Output, "(empty file)")
	}
}

func TestRead_OffsetPastEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path, "offset": 5})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for offset past EOF")
	}
	want := path + " has only 2 lines"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_MarksReadOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	tool := NewRead(OSFS(), tr, nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	if _, err := tool.Run(context.Background(), rcFor(dir), call); err != nil {
		t.Fatalf("Run: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.CheckWritable(core.SessionID("s1"), path, info); err != nil {
		t.Errorf("CheckWritable after read: got %v, want nil", err)
	}
}

func TestRead_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	tool := NewRead(OSFS(), NewTracker(), nil)
	call := core.ToolCall{ID: "c1", Name: "read", Input: json.RawMessage("{not json")}
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(res.Output, "invalid input: ") {
		t.Errorf("got %+v, want IsError with an 'invalid input:' prefix", res)
	}
}

func TestRead_MissingPath(t *testing.T) {
	dir := t.TempDir()
	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for missing path")
	}
}

func TestRead_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": dir})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}

func TestRead_NegativeOffsetRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path, "offset": -1})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a negative offset")
	}
	want := "offset must be >= 1"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_NegativeLimitRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path, "limit": -5})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a negative limit")
	}
	want := "limit must be >= 1"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_CRLFLinesStripped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\r\ntwo\r\nthree\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	want := "1: one\n2: two\n3: three"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestRead_RelativePathResolvesAgainstWorkDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewRead(OSFS(), NewTracker(), nil)
	call := mustCall(t, "read", map[string]any{"path": "f.txt"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if res.Output != "1: hi" {
		t.Errorf("got %q, want %q", res.Output, "1: hi")
	}
}
