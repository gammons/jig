// Package clock provides an indirection over wall-clock time so that
// callers can be tested deterministically with Fake instead of relying on
// time.Now and time.Sleep.
package clock

import "time"

// Clock is the interface all time-dependent code should depend on instead
// of calling time.Now or time.After directly.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

// Real returns a Clock backed by the actual wall clock and the standard
// library's time.After.
func Real() Clock {
	return realClock{}
}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}
