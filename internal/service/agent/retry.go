package agent

import (
	"context"
	"errors"
	"time"

	"github.com/gammons/jig/internal/core"
)

// maxAttempts is the total number of tries a step's request gets.
const maxAttempts = 3

// maxRetryAfter caps a provider's Retry-After, so a hostile or buggy
// value cannot stall a run for long.
const maxRetryAfter = 60 * time.Second

// stream consumes req into msg, retrying retryable provider errors that
// arrive before any stream event.
func (r *Runner) stream(ctx context.Context, st *run, req core.LLMRequest, msg *core.Message, tm *stepTiming) error {
	for attempt := 1; ; attempt++ {
		received, err := r.consume(ctx, st, req, msg, tm)
		if err == nil {
			return nil
		}
		delay, ok := retryDelay(err, received, attempt)
		giveUp := !ok || ctx.Err() != nil
		logAttempt(ctx, r.d.Log, attempt, received, err, delay, giveUp)
		if giveUp {
			return err
		}
		select {
		case <-r.d.Clock.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// retryDelay reports whether attempt's err may be retried and how long to
// wait first: the provider's RetryAfter if set (capped at maxRetryAfter),
// else 1s, then 2s.
func retryDelay(err error, received bool, attempt int) (time.Duration, bool) {
	if received || attempt >= maxAttempts {
		return 0, false
	}
	var le *core.LLMError
	if !errors.As(err, &le) || !le.Retryable {
		return 0, false
	}
	if le.RetryAfter > 0 {
		return min(le.RetryAfter, maxRetryAfter), true
	}
	return time.Duration(attempt) * time.Second, true
}
