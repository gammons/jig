package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

const (
	defaultBashTimeoutMS = 120000
	maxBashTimeoutMS     = 600000
	bashTailBytes        = 30 * 1024
)

// ShellSpec describes a command for Shell to run.
type ShellSpec struct {
	Command   string
	Dir       string
	Timeout   time.Duration
	SpillPath string
	TailBytes int
}

// ShellResult is the outcome of running a ShellSpec.
type ShellResult struct {
	Output     []byte
	ExitCode   int
	TimedOut   bool
	Truncated  bool
	TotalBytes int64
	SpillErr   error
}

// Shell is the process-execution capability bash needs.
type Shell interface {
	Run(ctx context.Context, spec ShellSpec) (ShellResult, error)
}

// bashInput is the JSON input bash accepts.
type bashInput struct {
	Command     string `json:"command"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	Description string `json:"description,omitempty"`
}

// bashTool implements ext.Tool for the "bash" tool.
type bashTool struct {
	sh      Shell
	tempDir string
}

// NewBash returns the "bash" tool, backed by sh. Full output over
// bashTailBytes is spilled to tempDir.
func NewBash(sh Shell, tempDir string) ext.Tool {
	return &bashTool{sh: sh, tempDir: tempDir}
}

func (b *bashTool) Name() string { return "bash" }

func (b *bashTool) Description() string {
	return "Run a shell command and return its combined stdout and stderr. " +
		"Output over 30 KB is truncated to its tail, with the full output " +
		"saved to a file. timeout_ms defaults to 120000 and is capped at 600000."
}

func (b *bashTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"command":     stringProp("The shell command to run."),
		"timeout_ms":  intProp("Timeout in milliseconds. Default 120000, max 600000."),
		"description": stringProp("A short (5-10 word) description of what this command does."),
	}, "command")
}

func (b *bashTool) Concurrent() bool { return false }

// Subject implements ext.Subjecter: the subject is the command string, or
// "" if input does not parse.
func (b *bashTool) Subject(_ ext.RunContext, input json.RawMessage) string {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ""
	}
	return in.Command
}

// Run implements ext.Tool.
func (b *bashTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in bashInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return core.ToolError(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Command == "" {
		return core.ToolError(call, "command is required"), nil
	}

	timeoutMS := clampTimeoutMS(in.TimeoutMS)
	spillPath := filepath.Join(b.tempDir, fmt.Sprintf("jig-bash-%s.log", call.ID))

	res, err := b.sh.Run(ctx, ShellSpec{
		Command:   in.Command,
		Dir:       rc.WorkDir,
		Timeout:   time.Duration(timeoutMS) * time.Millisecond,
		SpillPath: spillPath,
		TailBytes: bashTailBytes,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return core.ToolResult{}, ctxErr
		}
		return core.ToolError(call, fmt.Sprintf("bash failed: %v", err)), nil
	}

	return formatBashResult(call, res, spillPath, timeoutMS), nil
}

// clampTimeoutMS applies bash's timeout_ms default and cap: values <= 0
// use the default, and anything over the max is capped.
func clampTimeoutMS(ms int) int {
	if ms <= 0 {
		return defaultBashTimeoutMS
	}
	if ms > maxBashTimeoutMS {
		return maxBashTimeoutMS
	}
	return ms
}

// formatBashResult builds bash's ToolResult from a ShellResult: it
// prefixes a truncation notice (naming the spill path on success, or the
// underlying error if the spill failed) or removes the spill file when
// the output was not truncated, then appends an exit code or timeout
// marker.
func formatBashResult(call core.ToolCall, res ShellResult, spillPath string, timeoutMS int) core.ToolResult {
	output := string(res.Output)
	switch {
	case res.Truncated && res.SpillErr != nil:
		output = fmt.Sprintf("[output truncated; full output unavailable: %v]\n%s", res.SpillErr, output)
	case res.Truncated:
		output = fmt.Sprintf("[output truncated; full output: %s]\n%s", spillPath, output)
	default:
		_ = os.Remove(spillPath)
	}

	if res.TimedOut {
		output += fmt.Sprintf("\n[timed out after %ds]", timeoutMS/1000)
		return core.ToolError(call, output)
	}
	if res.ExitCode != 0 {
		output += fmt.Sprintf("\n[exit code %d]", res.ExitCode)
	}
	return core.ToolOK(call, output)
}
