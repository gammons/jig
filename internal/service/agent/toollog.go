package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/gammons/jig/internal/core"
)

// callOutcome says how a tool call's pipeline ended.
type callOutcome string

const (
	outcomeRan       callOutcome = "ran"       // the tool's Run was reached (or a panic)
	outcomeBlocked   callOutcome = "blocked"   // a Before hook blocked or failed
	outcomeUnknown   callOutcome = "unknown"   // the tool is not allowed
	outcomeInvalid   callOutcome = "invalid"   // the input is not a JSON object
	outcomeCancelled callOutcome = "cancelled" // ctx was cancelled before the tool ran
)

// logToolCall records one finished call. outBytes is the output size before
// capping; res.Output is already capped.
func logToolCall(ctx context.Context, log *slog.Logger, call core.ToolCall, res core.ToolResult, outcome callOutcome, dur time.Duration, outBytes int) {
	kv := []any{
		"tool", call.Name, "call_id", call.ID, "dur", dur,
		"out_bytes", outBytes, "out_bytes_capped", len(res.Output),
		"is_error", res.IsError, "blocked", outcome == outcomeBlocked,
	}
	switch outcome {
	case outcomeUnknown, outcomeInvalid, outcomeCancelled:
		kv = append(kv, "reason", string(outcome))
	}
	debug(ctx, log, "tool", "tool call", kv...)
}
