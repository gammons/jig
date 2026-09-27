package llm

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/gammons/jig/internal/core"
)

// retryAfterHeader is the HTTP response header a provider sets to name how
// many seconds a client should wait before retrying, e.g. on a 429.
const retryAfterHeader = "retry-after"

// MapError converts a *fantasy.ProviderError into a *core.LLMError,
// carrying across whether the error is retryable and, if the response set
// a "retry-after" header (looked up case-insensitively), how long to wait.
// An error that is not a *fantasy.ProviderError passes through unchanged.
func MapError(err error) error {
	var pe *fantasy.ProviderError
	if !errors.As(err, &pe) {
		return err
	}
	return &core.LLMError{
		Retryable:  pe.IsRetryable(),
		RetryAfter: retryAfter(pe.ResponseHeaders),
		Err:        pe,
	}
}

// retryAfter parses a "retry-after" header value, in seconds, out of
// headers. It returns 0 if there is no such header or it does not parse as
// an integer.
func retryAfter(headers map[string]string) time.Duration {
	for k, v := range headers {
		if !strings.EqualFold(k, retryAfterHeader) {
			continue
		}
		secs, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	return 0
}
