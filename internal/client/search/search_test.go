package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// buildFixture lays out a directory tree used by both TestGlob_Both and
// TestGrep_Both:
//
//	.git/                (empty; makes rg honor .gitignore)
//	.gitignore           ignores ignored.txt and *.log
//	a.go                 "TODO alpha" at line 2
//	pkg/b.go              "TODO beta" at line 2
//	pkg/sub/c.go          "TODO gamma" at line 2
//	notes.txt             "TODO delta" at line 1
//	ignored.txt           "TODO nope" at line 1 (gitignored)
//	debug.log             "TODO nope" at line 1 (gitignored via *.log)
//	.hidden.txt           "TODO hidden" at line 1 (dot-prefixed, always skipped
//	                      by the default, non-overriding invocations used here)
//
// The three .go files get distinct, explicit mtimes (oldest to newest:
// pkg/b.go, a.go, pkg/sub/c.go) so Glob's mtime-desc sort is unambiguous.
func buildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mustMkdir(t, filepath.Join(dir, ".git"))
	mustMkdir(t, filepath.Join(dir, "pkg", "sub"))

	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n*.log\n")
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n// TODO alpha\n")
	mustWrite(t, filepath.Join(dir, "pkg", "b.go"), "package pkg\n// TODO beta\n")
	mustWrite(t, filepath.Join(dir, "pkg", "sub", "c.go"), "package sub\n// TODO gamma\n")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "TODO delta\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "TODO nope\n")
	mustWrite(t, filepath.Join(dir, "debug.log"), "TODO nope\n")
	mustWrite(t, filepath.Join(dir, ".hidden.txt"), "TODO hidden\n")

	oldest := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	middle := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	mustChtimes(t, filepath.Join(dir, "pkg", "b.go"), oldest)
	mustChtimes(t, filepath.Join(dir, "a.go"), middle)
	mustChtimes(t, filepath.Join(dir, "pkg", "sub", "c.go"), newest)

	return dir
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func mustChtimes(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("Chtimes(%s): %v", path, err)
	}
}

// searcherVariants returns the searchers to run each "Both" test against:
// always NewWalker(), plus an rg-backed one if rg is on PATH.
func searcherVariants(t *testing.T) map[string]Searcher {
	t.Helper()
	variants := map[string]Searcher{"walker": NewWalker()}
	if _, err := exec.LookPath("rg"); err == nil {
		variants["rg"] = New(exec.LookPath)
	} else {
		t.Log("rg not on PATH; only exercising the walker fallback")
	}
	return variants
}

func TestGlob_Both(t *testing.T) {
	dir := buildFixture(t)
	want := []string{"pkg/sub/c.go", "a.go", "pkg/b.go"}

	for name, s := range searcherVariants(t) {
		t.Run(name, func(t *testing.T) {
			got, err := s.Glob(context.Background(), dir, "**/*.go", 0)
			if err != nil {
				t.Fatalf("Glob err = %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Glob(**/*.go) = %v, want %v", got, want)
			}
		})
	}
}

func TestGrep_Both(t *testing.T) {
	dir := buildFixture(t)

	t.Run("include filter", func(t *testing.T) {
		want := []Match{
			{Path: "a.go", Line: 2, Text: "// TODO alpha"},
			{Path: "pkg/b.go", Line: 2, Text: "// TODO beta"},
			{Path: "pkg/sub/c.go", Line: 2, Text: "// TODO gamma"},
		}
		for name, s := range searcherVariants(t) {
			t.Run(name, func(t *testing.T) {
				got, err := s.Grep(context.Background(), dir, "TODO", "*.go", 0)
				if err != nil {
					t.Fatalf("Grep err = %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("Grep(TODO, *.go) = %+v, want %+v", got, want)
				}
			})
		}
	})

	t.Run("limit", func(t *testing.T) {
		want := []Match{
			{Path: "a.go", Line: 2, Text: "// TODO alpha"},
			{Path: "notes.txt", Line: 1, Text: "TODO delta"},
		}
		for name, s := range searcherVariants(t) {
			t.Run(name, func(t *testing.T) {
				got, err := s.Grep(context.Background(), dir, "TODO", "", 2)
				if err != nil {
					t.Fatalf("Grep err = %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("Grep(TODO, limit=2) = %+v, want %+v", got, want)
				}
			})
		}
	})

	t.Run("gitignore honored", func(t *testing.T) {
		want := []Match{
			{Path: "a.go", Line: 2, Text: "// TODO alpha"},
			{Path: "notes.txt", Line: 1, Text: "TODO delta"},
			{Path: "pkg/b.go", Line: 2, Text: "// TODO beta"},
			{Path: "pkg/sub/c.go", Line: 2, Text: "// TODO gamma"},
		}
		for name, s := range searcherVariants(t) {
			t.Run(name, func(t *testing.T) {
				got, err := s.Grep(context.Background(), dir, "TODO", "", 0)
				if err != nil {
					t.Fatalf("Grep err = %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("Grep(TODO) = %+v, want %+v (ignored.txt/debug.log must be excluded)", got, want)
				}
			})
		}
	})
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "pkg/sub/c.go", true},
		{"**/*.go", "pkg/sub/c.txt", false},
		{"*.go", "pkg/sub/c.go", true},
		{"pkg/*.go", "pkg/b.go", true},
		{"pkg/*.go", "pkg/sub/c.go", false},
		{"**", "anything/at/all.txt", true},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.pattern, tt.path); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}
