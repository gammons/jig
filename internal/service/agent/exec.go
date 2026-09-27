package agent

import (
	"context"
	"fmt"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// execute runs calls sequentially against the allowed tools and returns one
// result per call it completed. An unknown tool or a tool's non-ctx error
// becomes an IsError result; on cancellation it stops early, leaving the
// remaining calls unanswered.
func (r *Runner) execute(ctx context.Context, rc ext.RunContext, allowed []ext.Tool, calls []core.ToolCall) []core.ToolResult {
	byName := make(map[string]ext.Tool, len(allowed))
	for _, t := range allowed {
		byName[t.Name()] = t
	}
	results := make([]core.ToolResult, 0, len(calls))
	for _, call := range calls {
		if ctx.Err() != nil {
			break
		}
		tool, ok := byName[call.Name]
		if !ok {
			results = append(results, core.ToolResult{
				CallID: call.ID, Name: call.Name, Output: fmt.Sprintf("unknown tool %q", call.Name), IsError: true,
			})
			continue
		}
		res, err := tool.Run(ctx, rc, call)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			res = core.ToolResult{Output: err.Error(), IsError: true}
		}
		res.CallID, res.Name = call.ID, call.Name
		results = append(results, res)
	}
	return results
}
