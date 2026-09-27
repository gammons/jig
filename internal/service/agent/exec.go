package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/event"
	"github.com/gammons/jig/internal/core/ext"
)

// cancelledOutput is the result text of a call cut short by cancellation.
const cancelledOutput = "cancelled"

// executor holds the per-step state execute shares across calls.
type executor struct {
	r       *Runner
	rc      ext.RunContext
	allowed []ext.Tool
	byName  map[string]ext.Tool
	hooks   []ext.ToolHook
}

// execute runs calls against the allowed tools and returns exactly one
// result per call, in call order. A maximal run of consecutive calls to
// Concurrent tools runs in parallel; every other call (non-concurrent,
// unknown tool, or invalid input) runs alone, in order. Each call is
// bracketed by ToolCallStarted/ToolCallFinished and passes through every
// registered ToolHook. Calls not yet started when ctx is cancelled, and
// calls whose hooks or tool observe the cancellation, get "cancelled".
func (r *Runner) execute(ctx context.Context, rc ext.RunContext, allowed []ext.Tool, calls []core.ToolCall) []core.ToolResult {
	ex := &executor{r: r, rc: rc, allowed: allowed, byName: make(map[string]ext.Tool, len(allowed)), hooks: r.d.Ext.ToolHooks()}
	for _, t := range allowed {
		ex.byName[t.Name()] = t
	}
	results := make([]core.ToolResult, len(calls))
	for i := 0; i < len(calls); {
		j := i + 1
		if ex.concurrent(calls[i]) {
			for j < len(calls) && ex.concurrent(calls[j]) {
				j++
			}
		}
		ex.batch(ctx, calls[i:j], results[i:j])
		i = j
	}
	return results
}

// concurrent reports whether call may join a parallel batch: its tool is
// allowed and Concurrent, and its input is valid.
func (ex *executor) concurrent(call core.ToolCall) bool {
	tool, ok := ex.byName[call.Name]
	if !ok || !tool.Concurrent() {
		return false
	}
	_, err := objectInput(call.Input)
	return err == nil
}

// batch runs calls, writing each result to the same index of out. A single
// call runs on the caller's goroutine; more run in parallel.
func (ex *executor) batch(ctx context.Context, calls []core.ToolCall, out []core.ToolResult) {
	if len(calls) == 1 {
		out[0] = ex.call(ctx, calls[0])
		return
	}
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Go(func() { out[i] = ex.call(ctx, call) })
	}
	wg.Wait()
}

// call runs one call through the full pipeline, publishing its start and
// finish, and returns its result stamped with the call's ID and Name.
func (ex *executor) call(ctx context.Context, call core.ToolCall) core.ToolResult {
	base := event.Base{SessionID: ex.rc.SessionID}
	ex.r.d.Bus.Publish(event.ToolCallStarted{Base: base, MessageID: ex.rc.MessageID, Call: call})
	res := ex.guarded(ctx, call)
	res.CallID, res.Name = call.ID, call.Name
	ex.r.d.Bus.Publish(event.ToolCallFinished{Base: base, MessageID: ex.rc.MessageID, Result: res})
	return res
}

// guarded runs resolveAndRun, turning a panic anywhere in the pipeline (a
// Before hook, the tool, or an After hook) into an error result, so one
// misbehaving extension cannot crash a parallel batch.
func (ex *executor) guarded(ctx context.Context, call core.ToolCall) (res core.ToolResult) {
	defer func() {
		if v := recover(); v != nil {
			res = errorResult(fmt.Sprintf("tool %s panicked: %v", call.Name, v))
		}
	}()
	return ex.resolveAndRun(ctx, call)
}

// resolveAndRun resolves call's tool, validates its input, runs the Before
// hooks, the tool, and the After hooks.
func (ex *executor) resolveAndRun(ctx context.Context, call core.ToolCall) core.ToolResult {
	if ctx.Err() != nil {
		return cancelledResult()
	}
	tool, ok := ex.byName[call.Name]
	if !ok {
		return errorResult(ex.unavailable(call.Name))
	}
	input, err := objectInput(call.Input)
	if err != nil {
		return errorResult(fmt.Sprintf("invalid JSON input for %s: %v", call.Name, err))
	}
	call.Input = input

	call, blocked, stop := ex.before(ctx, tool, call)
	if stop {
		return blocked
	}
	res := runTool(ctx, ex.rc, tool, call)
	for _, h := range ex.hooks {
		res = h.After(ctx, ex.rc, tool, call, res)
	}
	return res
}

// before runs every Before hook in order, threading rewritten calls
// through. It reports stop with the result to use when a hook blocks the
// call or fails.
func (ex *executor) before(ctx context.Context, tool ext.Tool, call core.ToolCall) (core.ToolCall, core.ToolResult, bool) {
	for _, h := range ex.hooks {
		next, verdict, err := h.Before(ctx, ex.rc, tool, call)
		switch {
		case err != nil && ctx.Err() != nil:
			return call, cancelledResult(), true
		case err != nil:
			return call, errorResult(err.Error()), true
		case verdict.Block:
			return call, errorResult(verdict.Reason), true
		}
		call = next
	}
	return call, core.ToolResult{}, false
}

// runTool runs tool, turning a ctx error into "cancelled" and any other
// error into an error result. Panics are recovered by guarded.
func runTool(ctx context.Context, rc ext.RunContext, tool ext.Tool, call core.ToolCall) core.ToolResult {
	res, err := tool.Run(ctx, rc, call)
	switch {
	case err != nil && ctx.Err() != nil:
		return cancelledResult()
	case err != nil:
		return errorResult(err.Error())
	}
	return res
}

// unavailable explains that name is not among the agent's allowed tools.
func (ex *executor) unavailable(name string) string {
	names := make([]string, len(ex.allowed))
	for i, t := range ex.allowed {
		names[i] = t.Name()
	}
	list := strings.Join(names, ", ")
	if list == "" {
		list = "(none)"
	}
	return fmt.Sprintf("tool %q is not available to agent %q; available: %s", name, ex.rc.Agent.Name, list)
}

// objectInput returns input when it decodes as a JSON object, treating
// empty input as {}.
func objectInput(input json.RawMessage) (json.RawMessage, error) {
	if len(strings.TrimSpace(string(input))) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("input is null, want a JSON object")
	}
	return input, nil
}

func errorResult(msg string) core.ToolResult {
	return core.ToolResult{Output: msg, IsError: true}
}

func cancelledResult() core.ToolResult {
	return errorResult(cancelledOutput)
}
