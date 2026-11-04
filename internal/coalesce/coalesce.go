// Package coalesce implements the in-flight/pending request-coalescing
// machine that two App refresh paths (git status, tmux activity scan) used
// to hand-roll separately. A request that arrives while work is in flight is
// recorded as a pending obligation (with a merged payload) instead of
// forking a second producer; the completion handler discharges the
// obligation by handing the merged payload back for exactly one follow-up.
//
// The state machine is declared once here and formally verified by
// exhaustive model checking in this package's tests; the runtime Entry steps
// that same table, so the checked model and the shipped behavior are one
// artifact.
package coalesce

import (
	"github.com/andyrewlee/amux/internal/fsm"
)

// State is the coalescer's control state. The two obligation states exist
// because the panic-latch reset may clear the in-flight mark while a real
// producer is still running: IdlePending is "no producer believed running,
// but work is still owed" — its result still arrives and still discharges
// the obligation.
type State uint8

const (
	// Idle: nothing running, nothing owed.
	Idle State = iota
	// InFlight: a producer is running, nothing owed.
	InFlight
	// InFlightPending: a producer is running and at least one request
	// arrived mid-flight (its payload merged).
	InFlightPending
	// IdlePending: no producer believed running, but a request is still
	// owed (reached only via the panic-latch reset).
	IdlePending
)

func (s State) String() string {
	switch s {
	case InFlight:
		return "in-flight"
	case InFlightPending:
		return "in-flight+pending"
	case IdlePending:
		return "idle+pending"
	default:
		return "idle"
	}
}

// Event is the coalescer's alphabet.
type Event uint8

const (
	// Request: a caller wants the work done (possibly again).
	Request Event = iota
	// Complete: a producer finished. The machine always lands on Idle and
	// hands the obligation (if any) back to the caller, who re-admits it
	// if it still wants the follow-up.
	Complete
	// PanicReset clears the in-flight mark after a panicking Update dropped
	// the producer's launch — but deliberately KEEPS any pending obligation
	// (obligation markers self-heal; see app_panic_latches.go).
	PanicReset
)

func (e Event) String() string {
	switch e {
	case Request:
		return "request"
	case Complete:
		return "complete"
	default:
		return "panic-reset"
	}
}

var machine = fsm.MustCompile(fsm.Machine[State, Event]{
	Name:    "request-coalescer",
	Initial: Idle,
	Steps: []fsm.Step[State, Event]{
		{From: Idle, Event: Request, To: InFlight},
		{From: InFlight, Event: Request, To: InFlightPending},
		{From: InFlightPending, Event: Request, To: InFlightPending},
		{From: IdlePending, Event: Request, To: InFlight},
		{From: InFlight, Event: Complete, To: Idle},
		{From: InFlightPending, Event: Complete, To: Idle},
		{From: IdlePending, Event: Complete, To: Idle},
		{From: InFlight, Event: PanicReset, To: Idle},
		{From: InFlightPending, Event: PanicReset, To: IdlePending},
		{From: IdlePending, Event: PanicReset, To: IdlePending},
		{From: Idle, Event: PanicReset, To: Idle},
	},
})

// Check formally verifies the coalescer table: every state reachable, a
// request never leaves the machine idle-with-nothing-owed, a completion
// always lands on Idle, and the panic reset never drops an obligation.
func Check() error {
	return machine.Check(fsm.Checker[State, Event]{
		Alphabet:             []Event{Request, Complete, PanicReset},
		AllDeclaredReachable: true,
		Edge: func(from State, event Event, to State) error {
			if event == Request && to == Idle {
				return errCoalesce("a request must start work or record an obligation")
			}
			if event == Complete && to != Idle {
				return errCoalesce("a completion must always land on idle (obligation is returned, not kept)")
			}
			if event == PanicReset && ((from == InFlightPending || from == IdlePending) && to == Idle) {
				return errCoalesce("panic reset must preserve a pending obligation")
			}
			return nil
		},
	})
}

type errCoalesce string

func (e errCoalesce) Error() string { return string(e) }

// Entry is one keyed coalescer (e.g. one workspace root). It is not
// synchronized: both call sites own it from the App's single-writer Update
// goroutine.
type Entry[T any] struct {
	state      State
	merge      func(a, b T) T
	pending    T
	hasPending bool
}

// New returns an idle entry that merges repeated payloads with merge.
func New[T any](merge func(a, b T) T) *Entry[T] {
	return &Entry[T]{state: Idle, merge: merge}
}

// Admit records a request. start is true when the caller must launch the
// producer now, carrying initial as its payload; false when the request
// coalesced into the pending obligation.
func (e *Entry[T]) Admit(payload T) (start bool, initial T) {
	next, ok := machine.Next(e.state, Request)
	if !ok {
		// Unreachable: every state handles Request.
		return false, initial
	}
	if next == InFlight {
		// Idle or IdlePending: start now; starting subsumes any surviving
		// obligation (merged into the launch payload so an owed
		// stronger request is never downgraded).
		e.state = InFlight
		if e.hasPending {
			payload = e.merge(e.pending, payload)
		}
		e.hasPending = false
		var zero T
		e.pending = zero
		return true, payload
	}
	// Mid-flight: merge into the pending obligation.
	e.state = next
	if e.hasPending {
		e.pending = e.merge(e.pending, payload)
	} else {
		e.pending = payload
		e.hasPending = true
	}
	return false, initial
}

// Complete discharges a producer result. rerun is true when a follow-up is
// owed; payload is the merged payload the follow-up should carry. The
// caller re-admits (Admit) to actually start it — the obligation is cleared
// here, so a caller that cannot run the follow-up simply does not.
func (e *Entry[T]) Complete() (rerun bool, payload T) {
	rerun = e.hasPending
	payload = e.pending
	if next, ok := machine.Next(e.state, Complete); ok {
		e.state = next
	}
	e.hasPending = false
	var zero T
	e.pending = zero
	return rerun, payload
}

// PanicReset clears the in-flight mark while keeping any pending obligation
// (the advisory-latch reset after a panicking Update).
func (e *Entry[T]) PanicReset() {
	if next, ok := machine.Next(e.state, PanicReset); ok {
		e.state = next
	}
}

// InFlight reports whether a producer is believed running.
func (e *Entry[T]) InFlight() bool {
	return e.state == InFlight || e.state == InFlightPending
}

// State exposes the control state for observability and tests.
func (e *Entry[T]) State() State { return e.state }
