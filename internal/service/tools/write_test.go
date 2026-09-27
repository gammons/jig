package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/jig/internal/core"
)

func TestWrite_NewFileNoReadNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	tool := NewWrite(OSFS(), NewTracker())
	call := mustCall(t, "write", map[string]any{"path": path, "content": "hello"})
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
	if string(got) != "hello" {
		t.Errorf("file content: got %q, want %q", got, "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("new file mode: got %v, want 0644", info.Mode().Perm())
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.txt")

	tool := NewWrite(OSFS(), NewTracker())
	call := mustCall(t, "write", map[string]any{"path": path, "content": "x"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestWrite_ExistingRequiresRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewWrite(OSFS(), NewTracker())
	call := mustCall(t, "write", map[string]any{"path": path, "content": "new"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("got IsError false, want true when overwriting an unread file")
	}
	if !strings.Contains(res.Output, "has not been read in this session") {
		t.Errorf("got %q, want a message about not having been read", res.Output)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Error("file was overwritten despite the refusal")
	}
}

func TestWrite_ExistingAfterReadSucceedsAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker()
	readTool := NewRead(OSFS(), tr)
	readCall := mustCall(t, "read", map[string]any{"path": path})
	if _, err := readTool.Run(context.Background(), rcFor(dir), readCall); err != nil {
		t.Fatalf("read Run: %v", err)
	}

	writeTool := NewWrite(OSFS(), tr)
	writeCall := mustCall(t, "write", map[string]any{"path": path, "content": "updated"})
	res, err := writeTool.Run(context.Background(), rcFor(dir), writeCall)
	if err != nil {
		t.Fatalf("write Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("existing file mode: got %v, want to be preserved as 0600", info.Mode().Perm())
	}
}

func TestWriteEdit_SubjectIsAbsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	writeTool := NewWrite(OSFS(), NewTracker())
	editTool := NewEdit(OSFS(), NewTracker())

	writeSubjecter, ok := writeTool.(interface{ Subject(json.RawMessage) string })
	if !ok {
		t.Fatal("write tool does not implement ext.Subjecter")
	}
	editSubjecter, ok := editTool.(interface{ Subject(json.RawMessage) string })
	if !ok {
		t.Fatal("edit tool does not implement ext.Subjecter")
	}

	input, err := json.Marshal(map[string]any{"path": path, "content": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := writeSubjecter.Subject(input); got != path {
		t.Errorf("write Subject: got %q, want %q", got, path)
	}

	editInput, err := json.Marshal(map[string]any{"path": path, "old_string": "a", "new_string": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := editSubjecter.Subject(editInput); got != path {
		t.Errorf("edit Subject: got %q, want %q", got, path)
	}

	if got := writeSubjecter.Subject(json.RawMessage("not json")); got != "" {
		t.Errorf("Subject on unparsable input: got %q, want \"\"", got)
	}
}

func TestWrite_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	tool := NewWrite(OSFS(), NewTracker())
	call := core.ToolCall{ID: "c1", Name: "write", Input: json.RawMessage("{not json")}
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(res.Output, "invalid input: ") {
		t.Errorf("got %+v, want IsError with an 'invalid input:' prefix", res)
	}
}

func TestWrite_MissingFields(t *testing.T) {
	dir := t.TempDir()
	tool := NewWrite(OSFS(), NewTracker())

	res, err := tool.Run(context.Background(), rcFor(dir), mustCall(t, "write", map[string]any{"content": "x"}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("missing path: got IsError false, want true")
	}

	res, err = tool.Run(context.Background(), rcFor(dir), mustCall(t, "write", map[string]any{"path": filepath.Join(dir, "f.txt")}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("missing content: got IsError false, want true")
	}
}

func TestWrite_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	tool := NewWrite(OSFS(), NewTracker())
	call := mustCall(t, "write", map[string]any{"path": filepath.Join(dir, "f.txt"), "content": "x"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}
