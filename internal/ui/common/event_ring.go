package common

import "time"

// EventRingCapacity bounds the session notification history. Sixty-four
// recent notifications is deep enough for "what did I miss" without
// unbounded growth — the ring is in-memory only by design.
const EventRingCapacity = 64

// SessionEvent is one captured notification — what a toast told the user
// and when. Type reuses ToastType so the recall surface can rank severities
// the same way the live toast styles them.
type SessionEvent struct {
	Time time.Time
	Type ToastType
	Text string
}

// EventRing is a fixed-capacity store of session notifications in
// arrival order. The toast model pushes into it at Show time — the single
// funnel every toast producer already flows through — and the recall
// overlay reads a newest-first Snapshot.
type EventRing struct {
	events []SessionEvent // oldest → newest, capped at EventRingCapacity
}

// Push records an event, dropping the oldest once capacity is reached.
func (r *EventRing) Push(e SessionEvent) {
	r.events = append(r.events, e)
	if len(r.events) > EventRingCapacity {
		// Drop whole slices, not one element, so long over-cap runs
		// amortize the copy — matches the transcript tail's trim shape.
		excess := len(r.events) - EventRingCapacity
		copy(r.events, r.events[excess:])
		r.events = r.events[:EventRingCapacity]
	}
}

// Snapshot returns the events newest-first — the recall overlay's render
// order. The slice is a copy; callers cannot mutate the ring through it.
func (r *EventRing) Snapshot() []SessionEvent {
	out := make([]SessionEvent, len(r.events))
	for i, e := range r.events {
		out[len(r.events)-1-i] = e
	}
	return out
}

// Len reports how many events are retained.
func (r *EventRing) Len() int { return len(r.events) }
