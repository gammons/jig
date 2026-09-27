package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// editInput is the JSON input edit accepts. OldString and NewString are
// pointers so a missing field can be distinguished from an explicit empty
// string.
type editInput struct {
	Path       string  `json:"path"`
	OldString  *string `json:"old_string"`
	NewString  *string `json:"new_string"`
	ReplaceAll bool    `json:"replace_all,omitempty"`
}

// editTool implements ext.Tool for the "edit" tool.
type editTool struct {
	fs FS
	tr *Tracker
}

// NewEdit returns the "edit" tool, backed by fs and tr.
func NewEdit(fs FS, tr *Tracker) ext.Tool {
	return &editTool{fs: fs, tr: tr}
}

func (e *editTool) Name() string { return "edit" }

func (e *editTool) Description() string {
	return "Replace an exact substring in an existing file. old_string must " +
		"match exactly once unless replace_all is set. Read the file first in " +
		"this session; edits are refused otherwise."
}

func (e *editTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringProp("Absolute path, or a path relative to the working directory."),
		"old_string":  stringProp("The exact text to find. Must be unique in the file unless replace_all is set."),
		"new_string":  stringProp("The text to replace old_string with."),
		"replace_all": boolProp("Replace every match instead of requiring a unique one. Default false."),
	}, "path", "old_string", "new_string")
}

func (e *editTool) Concurrent() bool { return false }

// Subject implements ext.Subjecter: the subject is the absolute path
// being edited, resolved the same way Run resolves it.
func (e *editTool) Subject(rc ext.RunContext, input json.RawMessage) string {
	return subjectPath(rc, input)
}

// Run implements ext.Tool.
func (e *editTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	in, errMsg := parseEditInput(call.Input)
	if errMsg != "" {
		return errResult(call, errMsg), nil
	}

	abs := resolvePath(rc.WorkDir, in.Path)

	info, statErr := e.fs.Stat(abs)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return errResult(call, abs+" does not exist"), nil
		}
		return errResult(call, statErr.Error()), nil
	}
	if err := e.tr.CheckWritable(rc.SessionID, abs, info); err != nil {
		return errResult(call, err.Error()), nil
	}

	content, err := e.fs.ReadFile(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	updated, count, errMsg := applyEdit(string(content), *in.OldString, *in.NewString, in.ReplaceAll)
	if errMsg != "" {
		return errResult(call, errMsg), nil
	}

	if err := e.fs.WriteFile(abs, []byte(updated), info.Mode()); err != nil {
		return errResult(call, err.Error()), nil
	}

	newInfo, err := e.fs.Stat(abs)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	e.tr.MarkRead(rc.SessionID, abs, newInfo)

	plural := ""
	if count != 1 {
		plural = "s"
	}
	return okResult(call, fmt.Sprintf("replaced %d occurrence%s in %s", count, plural, abs)), nil
}

// parseEditInput unmarshals raw into an editInput, returning an error
// message naming the problem instead of an editInput when raw is
// malformed or missing a required field.
func parseEditInput(raw json.RawMessage) (editInput, string) {
	var in editInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return editInput{}, fmt.Sprintf("invalid input: %v", err)
	}
	if in.Path == "" {
		return editInput{}, "path is required"
	}
	if in.OldString == nil {
		return editInput{}, "old_string is required"
	}
	if in.NewString == nil {
		return editInput{}, "new_string is required"
	}
	if *in.OldString == "" {
		return editInput{}, "old_string must not be empty"
	}
	if *in.OldString == *in.NewString {
		return editInput{}, "old_string and new_string must differ"
	}
	return in, ""
}

// applyEdit replaces old with new in content: every occurrence when
// replaceAll is set, otherwise exactly one, refusing when old is absent
// or appears more than once. It returns the updated content and the
// number of replacements made, or an error message.
func applyEdit(content, old, new string, replaceAll bool) (string, int, string) {
	count := strings.Count(content, old)
	switch {
	case count == 0:
		return "", 0, "old_string not found"
	case count > 1 && !replaceAll:
		return "", 0, fmt.Sprintf("old_string matches %d times; add context or set replace_all", count)
	case replaceAll:
		return strings.ReplaceAll(content, old, new), count, ""
	default:
		return strings.Replace(content, old, new, 1), 1, ""
	}
}
