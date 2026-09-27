package tools

import (
	"context"
	"strings"
	"testing"
)

func TestGrep_NoMatches(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{"pattern": "TODO"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "no matches" {
		t.Errorf("Output = %q, want %q", res.Output, "no matches")
	}
}

func TestGrep_ListsPathLineText(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{grepResult: []Match{
		{Path: "a.go", Line: 2, Text: "// TODO alpha"},
		{Path: "b.go", Line: 5, Text: "// TODO beta"},
	}}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{"pattern": "TODO", "include": "*.go"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "a.go:2: // TODO alpha\nb.go:5: // TODO beta"
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
	if s.gotInclude != "*.go" {
		t.Errorf("gotInclude = %q, want %q", s.gotInclude, "*.go")
	}
}

func TestGrep_Truncation(t *testing.T) {
	dir := t.TempDir()
	matches := make([]Match, 100)
	for i := range matches {
		matches[i] = Match{Path: "f.go", Line: i + 1, Text: "TODO"}
	}
	s := &fakeSearcher{grepResult: matches}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{"pattern": "TODO"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasSuffix(res.Output, "[truncated at 100 results]") {
		t.Errorf("Output does not end with the truncation notice: %q", res.Output)
	}
	if s.gotLimit != 100 {
		t.Errorf("limit = %d, want 100", s.gotLimit)
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("missing pattern: got IsError false, want true")
	}
}

func TestGrep_PathOutsideWorkDirRejected(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{"pattern": "TODO", "path": "../" + lastElem(other)})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Fatal("path outside workdir: got IsError false, want true")
	}
	if !strings.Contains(res.Output, "path must be inside the working directory or absolute") {
		t.Errorf("Output = %q, want the outside-workdir message", res.Output)
	}
}

func TestGrep_Concurrent(t *testing.T) {
	tool := NewGrep(&fakeSearcher{})
	if !tool.Concurrent() {
		t.Error("Concurrent() = false, want true")
	}
}

func TestGrep_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGrep(s)
	call := mustCall(t, "grep", map[string]any{"pattern": "TODO"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}
