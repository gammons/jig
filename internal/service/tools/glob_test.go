package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSearcher is a scripted Searcher for glob/grep tests.
type fakeSearcher struct {
	globResult []string
	grepResult []Match
	err        error

	gotDir     string
	gotPattern string
	gotInclude string
	gotLimit   int
}

func (f *fakeSearcher) Glob(_ context.Context, dir, pattern string, limit int) ([]string, error) {
	f.gotDir, f.gotPattern, f.gotLimit = dir, pattern, limit
	return f.globResult, f.err
}

func (f *fakeSearcher) Grep(_ context.Context, dir, pattern, include string, limit int) ([]Match, error) {
	f.gotDir, f.gotPattern, f.gotInclude, f.gotLimit = dir, pattern, include, limit
	return f.grepResult, f.err
}

func TestGlob_NoMatches(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if res.Output != "no matches" {
		t.Errorf("Output = %q, want %q", res.Output, "no matches")
	}
	if s.gotLimit != 100 {
		t.Errorf("limit = %d, want 100", s.gotLimit)
	}
}

func TestGlob_ListsOneLinePerMatch(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{globResult: []string{"a.go", "b.go"}}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "a.go\nb.go"
	if res.Output != want {
		t.Errorf("Output = %q, want %q", res.Output, want)
	}
}

func TestGlob_TruncationNotice(t *testing.T) {
	dir := t.TempDir()
	matches := make([]string, 100)
	for i := range matches {
		matches[i] = "f.go"
	}
	s := &fakeSearcher{globResult: matches}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasSuffix(res.Output, "[truncated at 100 results]") {
		t.Errorf("Output does not end with the truncation notice: %q", res.Output)
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("missing pattern: got IsError false, want true")
	}
}

func TestGlob_PathOutsideWorkDirRejected(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go", "path": "../" + lastElem(other)})
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

func TestGlob_AbsolutePathAllowedAnywhere(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go", "path": other})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError: %s", res.Output)
	}
	if s.gotDir != other {
		t.Errorf("gotDir = %q, want %q", s.gotDir, other)
	}
}

func TestGlob_SearcherError(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{err: errors.New("boom")}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go"})
	res, err := tool.Run(context.Background(), rcFor(dir), call)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("searcher error: got IsError false, want true")
	}
}

func TestGlob_RefusesCanceledContext(t *testing.T) {
	dir := t.TempDir()
	s := &fakeSearcher{}
	tool := NewGlob(s)
	call := mustCall(t, "glob", map[string]any{"pattern": "*.go"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Run(ctx, rcFor(dir), call)
	if err == nil {
		t.Fatal("got nil error, want ctx.Err() for a canceled context")
	}
}

func TestGlob_Concurrent(t *testing.T) {
	tool := NewGlob(&fakeSearcher{})
	if !tool.Concurrent() {
		t.Error("Concurrent() = false, want true")
	}
}

// lastElem returns the final path element of p, for building a "../x"
// relative path that points outside a t.TempDir() sibling.
func lastElem(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return p
	}
	return p[i+1:]
}
