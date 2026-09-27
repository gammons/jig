package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// grepInput is the JSON input grep accepts.
type grepInput struct {
	Pattern string `json:"pattern"`
	Include string `json:"include,omitempty"`
	Path    string `json:"path,omitempty"`
}

// grepTool implements ext.Tool for the "grep" tool.
type grepTool struct {
	s Searcher
}

// NewGrep returns the "grep" tool, backed by s.
func NewGrep(s Searcher) ext.Tool {
	return &grepTool{s: s}
}

func (g *grepTool) Name() string { return "grep" }

// Subject implements ext.Subjecter: the directory searched.
func (g *grepTool) Subject(rc ext.RunContext, input json.RawMessage) string {
	return searchSubject(rc, input)
}

func (g *grepTool) Description() string {
	return "Search file contents for a regular expression (RE2 syntax), " +
		"optionally restricted to files matching an include glob."
}

func (g *grepTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern": stringProp("RE2 regular expression to search for."),
		"include": stringProp("Glob restricting which files are searched, e.g. \"*.go\"."),
		"path":    stringProp("Directory to search in. Defaults to the working directory."),
	}, "pattern")
}

func (g *grepTool) Concurrent() bool { return true }

// Run implements ext.Tool.
func (g *grepTool) Run(ctx context.Context, rc ext.RunContext, call core.ToolCall) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	var in grepInput
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return core.ToolError(call, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if in.Pattern == "" {
		return core.ToolError(call, "pattern is required"), nil
	}

	dir, errMsg := resolveSearchPath(rc, in.Path)
	if errMsg != "" {
		return core.ToolError(call, errMsg), nil
	}

	matches, err := g.s.Grep(ctx, dir, in.Pattern, in.Include, searchLimit)
	if err != nil {
		return core.ToolError(call, err.Error()), nil
	}

	return core.ToolOK(call, formatGrepLines(matches, searchLimit)), nil
}

// formatGrepLines renders grep's "path:line: text" output, noting when
// the limit was hit and when there were no matches.
func formatGrepLines(matches []Match, limit int) string {
	if len(matches) == 0 {
		return "no matches"
	}
	lines := make([]string, len(matches))
	for i, m := range matches {
		lines[i] = fmt.Sprintf("%s:%d: %s", m.Path, m.Line, m.Text)
	}
	out := strings.Join(lines, "\n")
	if len(matches) == limit {
		out += "\n[truncated at 100 results]"
	}
	return out
}
