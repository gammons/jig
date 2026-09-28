package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gammons/jig/internal/bubbles/ansi"
	"github.com/gammons/jig/internal/bubbles/picker"
	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/ui/transcript"
)

// touchedFiles returns the paths the session touched — read, written, or
// edited (from the tool blocks), or changed — most recently touched
// first, normalized against workDir like the sidebar's.
func touchedFiles(workDir string, blocks []transcript.Block, changed []transcript.FileChange) []string {
	var in []transcript.FileChange
	for i := len(blocks) - 1; i >= 0; i-- {
		if p := touchedPath(blocks[i]); p != "" {
			in = append(in, transcript.FileChange{Path: p})
		}
	}
	for i := len(changed) - 1; i >= 0; i-- {
		in = append(in, changed[i])
	}
	norm := normalizeChanges(workDir, in)
	out := make([]string, len(norm))
	for i, c := range norm {
		out[i] = c.Path
	}
	return out
}

// touchedPath is the path a read, write, or edit tool block names, or "".
func touchedPath(b transcript.Block) string {
	if b.Kind != transcript.KindTool || b.Call == nil {
		return ""
	}
	switch b.Call.Name {
	case "read", "write", "edit":
	default:
		return ""
	}
	var in struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(b.Call.Input, &in) != nil {
		return ""
	}
	return in.Path
}

// fileItems ranks files: the touched ones (in touched order), then the
// git-modified ones, then the rest, each in port order. A path that isn't
// safe to show or to insert verbatim (control characters) is skipped.
func fileItems(files []core.ProjectFile, touched []string) []picker.Item {
	byPath := make(map[string]core.ProjectFile, len(files))
	for _, f := range files {
		if ansi.SanitizeLine(f.Path) == f.Path && f.Path != "" {
			byPath[f.Path] = f
		}
	}
	out := make([]picker.Item, 0, len(byPath))
	used := map[string]bool{}
	add := func(f core.ProjectFile) {
		if used[f.Path] {
			return
		}
		used[f.Path] = true
		it := picker.Item{ID: f.Path, Title: f.Path}
		if f.Modified {
			it.Detail = "modified"
		}
		out = append(out, it)
	}
	for _, p := range touched {
		if f, ok := byPath[p]; ok {
			add(f)
		}
	}
	for _, modified := range []bool{true, false} {
		for _, f := range files {
			if _, ok := byPath[f.Path]; ok && f.Modified == modified {
				add(f)
			}
		}
	}
	return out
}

// attachments returns the recorded paths whose "@<path>" token still
// occurs in text (R19), in recorded order, once each.
func attachments(text string, recorded []string) []string {
	var out []string
	for _, p := range recorded {
		if !slices.Contains(out, p) && hasToken(text, "@"+p) {
			out = append(out, p)
		}
	}
	return out
}

// hasToken reports whether tok occurs in text as a whole
// whitespace-delimited token.
func hasToken(text, tok string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], tok)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(tok)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (start == 0 || unicode.IsSpace(before)) && (end == len(text) || unicode.IsSpace(after)) {
			return true
		}
		from = start + 1
	}
}
