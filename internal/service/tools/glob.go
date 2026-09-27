package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// searchLimit bounds how many results glob and grep return.
const searchLimit = 100

// Match is one line found by Grep.
type Match struct {
	Path string
	Line int
	Text string
}

// Searcher finds files (Glob) and lines within them (Grep), scoped to a
// root directory.
type Searcher interface {
	Glob(ctx context.Context, dir, pattern string, limit int) ([]string, error)
	Grep(ctx context.Context, dir, pattern, include string, limit int) ([]Match, error)
}

// globInput is the JSON input glob accepts.
type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// globTool implements ext.Tool for the "glob" tool.
type globTool struct {
	s Searcher
}

// NewGlob returns the "glob" tool, backed by s.
func NewGlob(s Searcher) ext.Tool {
	return &globTool{s: s}
}

func (g *globTool) Name() string { return "glob" }

func (g *globTool) Description() string {
	return "Find files matching a glob pattern (e.g. \"**/*.go\"), most " +
		"recently modified first."
}

func (g *globTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern": stringProp("Glob pattern to match file paths against."),
		"path":    stringProp("Directory to search in. Defaults to the working directory."),
	}, "pattern")
}

func (g *globTool) Concurrent() bool { return true }

// Run implements ext.Tool.
func (g *globTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in globInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return errResult(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Pattern == "" {
		return errResult(call, "pattern is required"), nil
	}

	dir, errMsg := resolveSearchPath(rc, in.Path)
	if errMsg != "" {
		return errResult(call, errMsg), nil
	}

	paths, err := g.s.Glob(ctx, dir, in.Pattern, searchLimit)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	return okResult(call, formatMatchLines(paths, searchLimit)), nil
}

// resolveSearchPath resolves path (glob/grep's optional "path" input)
// against rc.WorkDir. An empty path means rc.WorkDir itself. A relative
// path must resolve inside rc.WorkDir; an absolute path is allowed
// anywhere. It returns an error message instead of a path when neither
// holds.
func resolveSearchPath(rc ext.RunContext, path string) (string, string) {
	if path == "" {
		return filepath.Clean(rc.WorkDir), ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), ""
	}

	abs := resolvePath(rc.WorkDir, path)
	root := filepath.Clean(rc.WorkDir)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", "path must be inside the working directory or absolute"
	}
	return abs, ""
}

// formatMatchLines renders glob's one-path-per-line output, noting when
// the limit was hit and when there were no matches.
func formatMatchLines(paths []string, limit int) string {
	if len(paths) == 0 {
		return "no matches"
	}
	out := strings.Join(paths, "\n")
	if len(paths) == limit {
		out += "\n[truncated at 100 results]"
	}
	return out
}
