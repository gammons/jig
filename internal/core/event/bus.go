package event

import (
	"sync"

	"github.com/gammons/jig/internal/core"
)

// Bus fans out published events to every active Subscription. Publish never
// blocks: each subscription owns its own pending queue and pump goroutine,
// so a slow or absent reader on one subscription cannot stall another, or
// the publisher.
type Bus struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// NewBus returns an empty Bus ready to accept subscribers.
func NewBus() *Bus {
	return &Bus{subs: make(map[*Subscription]struct{})}
}

// Publish appends e to every current subscription's pending queue. It never
// blocks on delivery.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	subs := make([]*Subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, s := range subs {
		s.enqueue(e)
	}
}

// Subscribe registers a new Subscription and starts its pump goroutine.
func (b *Bus) Subscribe() *Subscription {
	s := newSubscription(b)
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// remove drops s from the bus, so future Publish calls skip it.
func (b *Bus) remove(s *Subscription) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// Subscription delivers events from a Bus in order, one at a time, over an
// unbuffered channel. While its reader lags, adjacent TextDelta/
// ReasoningDelta events for the same message are coalesced in the pending
// queue instead of piling up unboundedly.
type Subscription struct {
	bus    *Bus
	mu     sync.Mutex
	queue  []Event
	notify chan struct{}
	out    chan Event
	done   chan struct{}
	closed sync.Once
}

func newSubscription(b *Bus) *Subscription {
	s := &Subscription{
		bus:    b,
		notify: make(chan struct{}, 1),
		out:    make(chan Event),
		done:   make(chan struct{}),
	}
	go s.pump()
	return s
}

// C returns the channel subscribers read events from. It is closed when the
// Subscription is closed.
func (s *Subscription) C() <-chan Event { return s.out }

// Close stops delivery, closes C(), and unregisters the subscription from
// its Bus. It is idempotent. Any events still in the pending queue are
// discarded.
func (s *Subscription) Close() {
	s.closed.Do(func() {
		s.bus.remove(s)
		close(s.done)
	})
}

// enqueue appends e to the pending queue, merging it into the last
// undelivered element when e is a TextDelta or ReasoningDelta matching that
// element's kind and message.
func (s *Subscription) enqueue(e Event) {
	s.mu.Lock()
	if !s.mergeLocked(e) {
		s.queue = append(s.queue, e)
	}
	s.mu.Unlock()

	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// mergeLocked reports whether e was merged into the queue's tail. Callers
// must hold s.mu.
func (s *Subscription) mergeLocked(e Event) bool {
	if len(s.queue) == 0 {
		return false
	}
	kind, msgID, text := deltaKindOf(e)
	if kind == notDelta {
		return false
	}
	last := len(s.queue) - 1
	lastKind, lastMsgID, lastText := deltaKindOf(s.queue[last])
	if lastKind != kind || lastMsgID != msgID {
		return false
	}
	s.queue[last] = withDeltaText(s.queue[last], lastText+text)
	return true
}

// dequeue pops the head of the queue, if any. Once an element is dequeued
// it is no longer reachable from mergeLocked, so it will never be mutated
// again, even while the pump is blocked handing it to a reader.
func (s *Subscription) dequeue() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil, false
	}
	e := s.queue[0]
	s.queue = s.queue[1:]
	return e, true
}

// pump forwards the pending queue, in order, to out, blocking on each send
// until the reader receives it or the subscription is closed.
func (s *Subscription) pump() {
	defer close(s.out)
	for {
		e, ok := s.dequeue()
		if !ok {
			select {
			case <-s.notify:
				continue
			case <-s.done:
				return
			}
		}
		select {
		case s.out <- e:
		case <-s.done:
			return
		}
	}
}

type deltaKind int

const (
	notDelta deltaKind = iota
	textDelta
	reasoningDelta
)

// deltaKindOf classifies e as a mergeable delta, returning its kind,
// message, and text. Non-delta events return notDelta.
func deltaKindOf(e Event) (deltaKind, core.MessageID, string) {
	switch v := e.(type) {
	case TextDelta:
		return textDelta, v.MessageID, v.Text
	case ReasoningDelta:
		return reasoningDelta, v.MessageID, v.Text
	default:
		return notDelta, "", ""
	}
}

// withDeltaText returns e with its Text field replaced by text. e must be a
// TextDelta or ReasoningDelta.
func withDeltaText(e Event, text string) Event {
	switch v := e.(type) {
	case TextDelta:
		v.Text = text
		return v
	case ReasoningDelta:
		v.Text = text
		return v
	default:
		return e
	}
}
