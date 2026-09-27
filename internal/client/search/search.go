// Package search finds files and text within them: New prefers an
// external ripgrep binary and falls back to a pure-Go walker (NewWalker)
// when rg is not on PATH. Both implementations skip hidden files and
// directories (dot-prefixed) and .git, and honor the root .gitignore.
package search

import (
	"context"
	"sort"
	"time"
)

// maxLineChars bounds a Match's Text: longer lines keep their first
// maxLineChars characters followed by longLineNote.
const maxLineChars = 500

// longLineNote marks a Match whose line was cut at maxLineChars, as rg's
// --max-columns-preview prints it.
const longLineNote = " [... omitted end of long line]"

// Match is one line found by Grep.
type Match struct {
	Path string
	Line int
	Text string
}

// Searcher finds files (Glob) and lines within them (Grep) under a root
// directory. Returned paths are relative to dir and slash-separated.
type Searcher interface {
	// Glob returns paths matching pattern, sorted by mtime descending
	// (path ascending on ties), truncated to limit if limit > 0.
	Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error)
	// Grep returns lines matching the RE2 pattern, optionally restricted
	// to files matching the include glob, sorted by path then line and
	// truncated to limit if limit > 0.
	Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error)
}

// New returns an rg-backed Searcher if lookPath("rg") succeeds, else the
// Go fallback (NewWalker). Production code passes exec.LookPath.
func New(lookPath func(string) (string, error)) Searcher {
	if path, err := lookPath("rg"); err == nil {
		return &rgSearcher{rgPath: path}
	}
	return NewWalker()
}

// NewWalker returns the pure-Go fallback Searcher, used directly by tests
// and whenever rg is unavailable.
func NewWalker() Searcher {
	return &walker{}
}

// fileEntry is a Glob candidate carrying enough to sort by mtime desc,
// path asc before truncating to a limit.
type fileEntry struct {
	path  string
	mtime time.Time
}

// sortAndLimitPaths sorts entries by mtime descending (path ascending on
// ties) and returns their paths, truncated to limit if limit > 0.
func sortAndLimitPaths(entries []fileEntry, limit int) []string {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].mtime.Equal(entries[j].mtime) {
			return entries[i].mtime.After(entries[j].mtime)
		}
		return entries[i].path < entries[j].path
	})
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.path
	}
	return applyLimit(paths, limit)
}

// sortMatches sorts matches by path then line, in place, and returns them.
func sortMatches(matches []Match) []Match {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	return matches
}

// applyLimit truncates items to limit entries when limit > 0; limit <= 0
// means no limit.
func applyLimit[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}
