package clock

import (
	"sync"
	"time"
)

// waiter is a pending After call: it fires ch once the fake clock's now
// reaches or passes deadline.
type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

// Fake is a controllable Clock for tests. All state is guarded by mu; cond
// lets BlockUntilWaiters wait for After calls without polling.
type Fake struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	waiters []waiter
}

// NewFake returns a Fake clock whose Now() starts at start.
func NewFake(start time.Time) *Fake {
	f := &Fake{now: start}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Now returns the fake clock's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After returns a channel that receives the deadline time once Advance
// moves the fake clock's now to or past it.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan time.Time, 1)
	f.waiters = append(f.waiters, waiter{deadline: f.now.Add(d), ch: ch})
	f.cond.Broadcast()
	return ch
}

// Advance moves the fake clock's now forward by d, firing every waiter
// whose deadline has been reached.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.now = f.now.Add(d)

	remaining := f.waiters[:0]
	for _, w := range f.waiters {
		if !w.deadline.After(f.now) {
			w.ch <- w.deadline
		} else {
			remaining = append(remaining, w)
		}
	}
	f.waiters = remaining
}

// BlockUntilWaiters blocks until at least n After() calls are pending.
func (f *Fake) BlockUntilWaiters(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for len(f.waiters) < n {
		f.cond.Wait()
	}
}
