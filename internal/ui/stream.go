package ui

import "time"

// streamState is the streamTick's state: whether it is running, and
// subStale, set by a child event while the details split shows a running
// subagent (the next tick re-reads its messages).
type streamState struct {
	ticking  bool
	subStale bool
}

// nextInterval is the delay after a render pass that took took: 3× it,
// clamped to [streamInterval, maxStreamInterval].
func nextInterval(took time.Duration) time.Duration {
	return min(max(3*took, streamInterval), maxStreamInterval)
}
