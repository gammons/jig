package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

const (
	defaultReadLimit = 2000
	maxLineChars     = 2000
	binarySniffBytes = 8192
)

// readInput is the JSON input read accepts.
type readInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// readTool implements ext.Tool for the "read" tool.
type readTool struct {
	fs FS
	tr *Tracker
}

// NewRead returns the "read" tool, backed by fs and tr.
func NewRead(fs FS, tr *Tracker) ext.Tool {
	return &readTool{fs: fs, tr: tr}
}

func (r *readTool) Name() string { return "read" }

func (r *readTool) Description() string {
	return "Read a file or list a directory. File output is line-numbered " +
		"(\"<n>: <line>\"), 1-based. Read a file before writing or editing it."
}

func (r *readTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":   stringProp("Absolute path, or a path relative to the working directory."),
		"offset": intProp("1-based line number to start from. Default 1."),
		"limit":  intProp("Maximum number of lines to return. Default 2000."),
	}, "path")
}

func (r *readTool) Concurrent() bool { return true }

// Run implements ext.Tool.
func (r *readTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in readInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return errResult(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Path == "" {
		return errResult(call, "path is required"), nil
	}

	abs := resolvePath(rc.WorkDir, in.Path)

	info, err := r.fs.Stat(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	if info.IsDir() {
		return r.readDir(call, abs)
	}
	return r.readFile(call, rc, abs, in, info)
}

// readDir lists abs's entries, one per line, with a trailing "/" on
// subdirectories. Directory reads do not mark the tracker.
func (r *readTool) readDir(call core.ToolCall, abs string) (core.ToolResult, error) {
	entries, err := r.fs.ReadDir(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	if len(entries) == 0 {
		return okResult(call, "(empty directory)"), nil
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	return okResult(call, strings.Join(lines, "\n")), nil
}

// readFile reads abs, checks it for binary content, and formats the
// requested window of lines. On success it marks abs as read in the
// tracker.
func (r *readTool) readFile(call core.ToolCall, rc ext.RunContext, abs string, in readInput, info fs.FileInfo) (core.ToolResult, error) {
	content, err := r.fs.ReadFile(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	if looksBinary(content) {
		return errResult(call, abs+" appears to be binary"), nil
	}

	out, ierr := formatLines(abs, content, in.Offset, in.Limit)
	if ierr != "" {
		return errResult(call, ierr), nil
	}

	r.tr.MarkRead(rc.SessionID, abs, info)
	return okResult(call, out), nil
}

// looksBinary reports whether content's first binarySniffBytes contain a
// NUL byte.
func looksBinary(content []byte) bool {
	n := len(content)
	if n > binarySniffBytes {
		n = binarySniffBytes
	}
	for _, b := range content[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// splitLines splits content into lines the way a line-oriented editor
// would: a single trailing newline does not produce an extra empty final
// line. An empty file yields no lines.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(content), "\n")
	return strings.Split(s, "\n")
}

// formatLines renders the [offset, offset+limit) window of content's lines
// as "<n>: <line>", 1-based, truncating long lines and noting when more
// lines remain. It returns an error message (and no output) if offset is
// past the end of the file.
func formatLines(path string, content []byte, offset, limit int) (string, string) {
	lines := splitLines(content)
	if len(lines) == 0 {
		return "(empty file)", ""
	}

	if offset == 0 {
		offset = 1
	}
	if limit == 0 {
		limit = defaultReadLimit
	}
	if offset > len(lines) {
		return "", fmt.Sprintf("%s has only %d lines", path, len(lines))
	}

	start := offset - 1
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}

	out := make([]string, 0, end-start+1)
	for i := start; i < end; i++ {
		out = append(out, fmt.Sprintf("%d: %s", offset+i-start, truncateLine(lines[i])))
	}
	if end < len(lines) {
		out = append(out, fmt.Sprintf("(more lines; continue with offset %d)", end+1))
	}
	return strings.Join(out, "\n"), ""
}

// truncateLine truncates line to maxLineChars runes, appending "…" if it
// was longer.
func truncateLine(line string) string {
	runes := []rune(line)
	if len(runes) <= maxLineChars {
		return line
	}
	return string(runes[:maxLineChars]) + "…"
}
