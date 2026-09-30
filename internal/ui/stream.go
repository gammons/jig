package ui

import "time"

// streamState is the streamTick's state: whether it is running.
type streamState struct {
	ticking bool
}

// nextInterval is the delay after a render pass that took took: 3× it,
// clamped to [streamInterval, maxStreamInterval].
func nextInterval(took time.Duration) time.Duration {
	return min(max(3*took, streamInterval), maxStreamInterval)
}
