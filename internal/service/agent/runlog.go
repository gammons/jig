package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/core/ext"
)

// stepTiming records when the latest stream attempt of a step started and
// when its first event arrived (zero if none did).
type stepTiming struct {
	start, firstEvent time.Time
}

// firstEventDelay is the time from the attempt's start to its first event,
// or 0 if none arrived.
func (t *stepTiming) firstEventDelay() time.Duration {
	if t.firstEvent.IsZero() {
		return 0
	}
	return t.firstEvent.Sub(t.start)
}

// runStats accumulates what "run end" reports.
type runStats struct {
	started time.Time
	steps   int
	usage   core.Usage
	cost    float64
	maxed   bool
}

// withRunLog returns ctx carrying the attrs every record of a run has.
func withRunLog(ctx context.Context, rc ext.RunContext) context.Context {
	root := rc.RootID
	if root == "" {
		root = rc.SessionID
	}
	return core.WithLogAttrs(ctx,
		slog.String("root", string(root)),
		slog.String("session", string(rc.SessionID)),
		slog.Int("depth", rc.Depth),
		slog.String("agent", rc.Agent.Name),
	)
}

// debug logs msg at debug level under category cat, with the run's ctx attrs
// followed by kv.
func debug(ctx context.Context, log *slog.Logger, cat, msg string, kv ...any) {
	args := append([]any{"cat", cat}, core.LogArgs(ctx)...)
	log.Debug(msg, append(args, kv...)...)
}

func logRunEnd(ctx context.Context, log *slog.Logger, now time.Time, rs *runStats, err error) {
	outcome := "done"
	switch {
	case err != nil && ctx.Err() != nil:
		outcome = "cancelled"
	case err != nil:
		outcome = "failed"
	case rs.maxed:
		outcome = "max_steps"
	}
	kv := []any{
		"outcome", outcome, "steps", rs.steps,
		"in", rs.usage.Input, "out", rs.usage.Output, "cost", rs.cost,
		"dur", now.Sub(rs.started),
	}
	if err != nil {
		kv = append(kv, "err", err)
	}
	debug(ctx, log, "run", "run end", kv...)
}

func logStepEnd(ctx context.Context, log *slog.Logger, n int, msg core.Message, tm *stepTiming, stream, tools time.Duration) {
	debug(ctx, log, "step", "step end",
		"step", n,
		"first_event", tm.firstEventDelay(),
		"stream", stream,
		"tools_dur", tools,
		"in", msg.Usage.Input, "out", msg.Usage.Output,
		"cache_read", msg.Usage.CacheRead, "cache_write", msg.Usage.CacheWrite,
		"calls", len(toolCalls(msg)),
		"status", string(msg.Status),
	)
}

func logAttempt(ctx context.Context, log *slog.Logger, attempt int, received bool, err error, delay time.Duration, giveUp bool) {
	var retryable bool
	var after time.Duration
	var le *core.LLMError
	if errors.As(err, &le) {
		retryable, after = le.Retryable, le.RetryAfter
	}
	kv := []any{"attempt", attempt, "received", received, "retryable", retryable, "retry_after", after}
	if giveUp {
		kv = append(kv, "giving_up", true)
	} else {
		kv = append(kv, "delay", delay)
	}
	debug(ctx, log, "retry", "attempt failed", append(kv, "err", err)...)
}
