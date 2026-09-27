package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gammons/jig/internal/core"
)

// readThenGet reads path with the read tool under tr/sid, so a subsequent
// write or edit passes the read-before-write check.
func readThenGet(t *testing.T, tr *Tracker, dir, path string) {
	t.Helper()
	readTool := NewRead(OSFS(), tr)
	call := mustCall(t, "read", map[string]any{"path": path})
	res, err := readTool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("read Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("read IsError: %s", res.Output)
	}
}

func TestEdit_UniqueReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "world", "new_string": "there"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello there" {
		t.Errorf("got %q, want %q", got, "hello there")
	}
	if !strings.Contains(res.Output, "1") {
		t.Errorf("output should mention the replacement count: %q", res.Output)
	}
}

func TestEdit_MultipleMatchesError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a a a"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "a", "new_string": "b"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for multiple matches without replace_all")
	}
	want := "old_string matches 3 times; add context or set replace_all"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a a a" {
		t.Error("file was modified despite the refusal")
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a a a"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "a", "new_string": "b", "replace_all": true})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "b b b" {
		t.Errorf("got %q, want %q", got, "b b b")
	}
	if !strings.Contains(res.Output, "3") {
		t.Errorf("output should mention the replacement count: %q", res.Output)
	}
}

func TestEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "goodbye", "new_string": "hi"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true when old_string is not found")
	}
	if !strings.Contains(res.Output, "not found") {
		t.Errorf("got %q, want a message containing \"not found\"", res.Output)
	}
}

func TestEdit_RefusesWhenFileChangedSinceRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	newMod := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, newMod, newMod); err != nil {
		t.Fatal(err)
	}

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "world", "new_string": "there"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a file changed since it was read")
	}
	if !strings.Contains(res.Output, "modified since it was last read") {
		t.Errorf("got %q, want a message about the file having changed", res.Output)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Error("file was modified despite the refusal")
	}
}

func TestEdit_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.txt")

	tool := NewEdit(OSFS(), NewTracker())
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "a", "new_string": "b"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true for a non-existent file")
	}
	want := path + " does not exist"
	if res.Output != want {
		t.Errorf("got %q, want %q", res.Output, want)
	}
}

func TestEdit_RequiresRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewEdit(OSFS(), NewTracker())
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "world", "new_string": "there"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true when the file has not been read")
	}
	if !strings.Contains(res.Output, "has not been read in this session") {
		t.Errorf("got %q, want a message about not having been read", res.Output)
	}
}

func TestEdit_SameOldAndNewString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readThenGet(t, tr, dir, path)

	tool := NewEdit(OSFS(), tr)
	call := mustCall(t, "edit", map[string]any{"path": path, "old_string": "world", "new_string": "world"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true when old_string equals new_string")
	}
}

func TestEdit_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	tool := NewEdit(OSFS(), NewTracker())
	call := core.ToolCall{ID: "c1", Name: "edit", Input: json.RawMessage("{not json")}
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(res.Output, "invalid input: ") {
		t.Errorf("got %+v, want IsError with an 'invalid input:' prefix", res)
	}
}

func TestEdit_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	tool := NewEdit(OSFS(), NewTracker())
	call := mustCall(t, "edit", map[string]any{"path": filepath.Join(dir, "f.txt"), "old_string": "a", "new_string": "b"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}
