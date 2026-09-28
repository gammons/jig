package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

const (
	defaultReadLimit = 2000
	maxLineChars     = 2000
	binarySniffBytes = 8192
	maxReadBytes     = 50 * 1024
	// readNoteReserve is subtracted from maxReadBytes when budgeting the
	// line window, so the "(output truncated at 50 KB; continue with
	// offset N)" note formatLines appends always fits inside the
	// executor's own 50 KB cap (and its own truncation notice).
	readNoteReserve = 96
	maxImageBytes   = 20 << 20 // largest image read accepts, by file size
)

// readInput is the JSON input read accepts.
type readInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Imager turns raw image bytes into a bounded, stored core.Media. It is
// satisfied by *media.Pipeline; tools depends on it through this
// interface rather than importing service/media directly.
type Imager interface {
	Process(data []byte) (core.Media, core.ImageInfo, error)
}

// readTool implements ext.Tool for the "read" tool.
type readTool struct {
	fs  FS
	tr  *Tracker
	img Imager
}

// NewRead returns the "read" tool, backed by fs and tr. img is used to
// decode image paths into core.Media; a nil img keeps images being read
// as binary (and refused), as before images were supported.
func NewRead(fs FS, tr *Tracker, img Imager) ext.Tool {
	return &readTool{fs: fs, tr: tr, img: img}
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

// Subject implements ext.Subjecter: the absolute path being read, resolved
// the same way Run resolves it.
func (r *readTool) Subject(rc ext.RunContext, input json.RawMessage) string {
	return subjectPath(rc, input)
}

// Run implements ext.Tool.
func (r *readTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in readInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return core.ToolError(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Path == "" {
		return core.ToolError(call, "path is required"), nil
	}
	if in.Offset < 0 {
		return core.ToolError(call, "offset must be >= 1"), nil
	}
	if in.Limit < 0 {
		return core.ToolError(call, "limit must be >= 1"), nil
	}

	abs := resolvePath(rc.WorkDir, in.Path)

	info, err := r.fs.Stat(abs)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}
	if info.IsDir() {
		return r.readDir(call, abs)
	}
	if r.img != nil && isImagePath(abs) {
		return r.readImage(call, abs, info)
	}
	return r.readFile(call, rc, abs, in, info)
}

// isImagePath reports whether path has an image extension Imager
// accepts. It mirrors media.IsImagePath without importing service/media.
func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// readImage refuses files over maxImageBytes, then runs abs's bytes
// through r.img and reports the decoded image's dimensions and size. It
// does not mark abs as read in the tracker: images are not editable.
func (r *readTool) readImage(call core.ToolCall, abs string, info fs.FileInfo) (core.ToolResult, error) {
	if info.Size() > maxImageBytes {
		return core.ToolError(call, "image too large"), nil
	}
	data, err := r.fs.ReadFile(abs)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}
	m, imgInfo, err := r.img.Process(data)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}
	res := core.ToolOK(call, fmt.Sprintf("image %dx%d (%s)", imgInfo.Width, imgInfo.Height, humanBytes(imgInfo.Bytes)))
	res.Media = []core.Media{m}
	return res, nil
}

// humanBytes renders n as a human-readable size: bytes below 1024,
// kilobytes below 1 MiB, else megabytes to one decimal place.
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", n/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}

// readDir lists abs's entries, one per line, with a trailing "/" on
// subdirectories. Directory reads do not mark the tracker.
func (r *readTool) readDir(call core.ToolCall, abs string) (core.ToolResult, error) {
	entries, err := r.fs.ReadDir(abs)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}
	if len(entries) == 0 {
		return core.ToolOK(call, "(empty directory)"), nil
	}

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	return core.ToolOK(call, strings.Join(lines, "\n")), nil
}

// readFile reads abs, checks it for binary content, and formats the
// requested window of lines. On success it marks abs as read in the
// tracker.
func (r *readTool) readFile(call core.ToolCall, rc ext.RunContext, abs string, in readInput, info fs.FileInfo) (core.ToolResult, error) {
	content, err := r.fs.ReadFile(abs)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}
	if looksBinary(content) {
		return core.ToolError(call, abs+" appears to be binary"), nil
	}

	out, ierr := formatLines(abs, content, in.Offset, in.Limit)
	if ierr != "" {
		return core.ToolError(call, ierr), nil
	}

	r.tr.MarkRead(rc.SessionID, abs, info)
	return core.ToolOK(call, out), nil
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
// line, and a trailing "\r" (CRLF line endings) is stripped from every
// line so CRLF files render cleanly. An empty file yields no lines.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(content), "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// formatLines renders the [offset, offset+limit) window of content's lines
// as "<n>: <line>", 1-based, truncating long lines and noting when more
// lines remain. It stops adding lines once the output would exceed
// maxReadBytes, noting the offset to continue from. It returns an error message (and no output) if offset is
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

	var b strings.Builder
	for i := start; i < end; i++ {
		line := fmt.Sprintf("%d: %s", i+1, truncateLine(lines[i]))
		if i > start && b.Len()+1+len(line) > maxReadBytes-readNoteReserve {
			fmt.Fprintf(&b, "\n(output truncated at 50 KB; continue with offset %d)", i+1)
			return b.String(), ""
		}
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "\n(more lines; continue with offset %d)", end+1)
	}
	return b.String(), ""
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
