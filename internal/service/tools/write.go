package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

const (
	newFileMode = fs.FileMode(0o644)
	newDirMode  = fs.FileMode(0o755)
)

// writeInput is the JSON input write accepts. Content is a pointer so a
// missing field can be distinguished from an explicit empty string.
type writeInput struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

// writeTool implements ext.Tool for the "write" tool.
type writeTool struct {
	fs FS
	tr *Tracker
}

// NewWrite returns the "write" tool, backed by fs and tr.
func NewWrite(fs FS, tr *Tracker) ext.Tool {
	return &writeTool{fs: fs, tr: tr}
}

func (w *writeTool) Name() string { return "write" }

func (w *writeTool) Description() string {
	return "Write content to a file, creating it (and its parent directories) " +
		"if needed. Overwriting an existing file requires having read it first " +
		"in this session."
}

func (w *writeTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringProp("Absolute path, or a path relative to the working directory."),
		"content": stringProp("The full content to write to the file."),
	}, "path", "content")
}

func (w *writeTool) Concurrent() bool { return false }

// Subject implements ext.Subjecter: the subject is the absolute path
// being written, resolved the same way Run resolves it.
func (w *writeTool) Subject(rc ext.RunContext, input json.RawMessage) string {
	return subjectPath(rc, input)
}

// Run implements ext.Tool.
func (w *writeTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in writeInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return errResult(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Path == "" {
		return errResult(call, "path is required"), nil
	}
	if in.Content == nil {
		return errResult(call, "content is required"), nil
	}

	abs := resolvePath(rc.WorkDir, in.Path)

	mode := newFileMode
	existing, statErr := w.fs.Stat(abs)
	switch {
	case statErr == nil:
		if err := w.tr.CheckWritable(rc.SessionID, abs, existing); err != nil {
			return errResult(call, err.Error()), nil
		}
		mode = existing.Mode()
	case os.IsNotExist(statErr):
		// New file: no prior read required.
	default:
		return errResult(call, statErr.Error()), nil
	}

	if err := w.fs.MkdirAll(filepath.Dir(abs), newDirMode); err != nil {
		return errResult(call, err.Error()), nil
	}
	if err := w.fs.WriteFile(abs, []byte(*in.Content), mode); err != nil {
		return errResult(call, err.Error()), nil
	}

	newInfo, err := w.fs.Stat(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	w.tr.MarkRead(rc.SessionID, abs, newInfo)

	return okResult(call, fmt.Sprintf("wrote %d bytes to %s", len(*in.Content), abs)), nil
}
