package agent

import (
	"context"
	"errors"
	"time"

	"github.com/gammons/jig/internal/core"
)

// maxAttempts is the total number of tries a step's request gets.
const maxAttempts = 3

// stream consumes req into msg, retrying retryable provider errors that
// arrive before any stream event.
func (r *Runner) stream(ctx context.Context, st *run, req core.LLMRequest, msg *core.Message) error {
	for attempt := 1; ; attempt++ {
		received, err := r.consume(ctx, st, req, msg)
		if err == nil {
			return nil
		}
		delay, ok := retryDelay(err, received, attempt)
		if !ok || ctx.Err() != nil {
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
// wait first: the provider's RetryAfter if set, else 1s, then 2s.
func retryDelay(err error, received bool, attempt int) (time.Duration, bool) {
	if received || attempt >= maxAttempts {
		return 0, false
	}
	var le *core.LLMError
	if !errors.As(err, &le) || !le.Retryable {
		return 0, false
	}
	if le.RetryAfter > 0 {
		return le.RetryAfter, true
	}
	return time.Duration(attempt) * time.Second, true
}
