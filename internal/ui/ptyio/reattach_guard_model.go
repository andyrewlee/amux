package ptyio

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// guardState is the reattach guard's control state, derived from the
// exported fields (the fields are the extended state; the derivation below
// is the single mapping between them and the protocol machine).
type guardState int

const (
	// guardIdle: no reattach attempt in flight.
	guardIdle guardState = iota
	// guardHeldStamped: an attempt is in flight with an acquisition stamp.
	guardHeldStamped
	// guardHeldUnstamped: the in-flight flag is set but carries no stamp —
	// the defensive state for a flag set outside Begin; the next sweep
	// stamps it rather than releasing something that never timed.
	guardHeldUnstamped
)

func (s guardState) String() string {
	switch s {
	case guardHeldStamped:
		return "held-stamped"
	case guardHeldUnstamped:
		return "held-unstamped"
	default:
		return "idle"
	}
}

// guardEvent is the guard protocol's alphabet.
type guardEvent int

const (
	evtGuardBegin guardEvent = iota
	evtFlagSetWithoutBegin
	evtGuardSweepFresh
	evtGuardSweepExpired
	evtGuardSweepRunning
	evtGuardSweepUnstamped
	evtGuardRelease
)

func (e guardEvent) String() string {
	switch e {
	case evtGuardBegin:
		return "begin"
	case evtFlagSetWithoutBegin:
		return "flag-set-without-begin"
	case evtGuardSweepFresh:
		return "sweep-fresh"
	case evtGuardSweepExpired:
		return "sweep-expired"
	case evtGuardSweepRunning:
		return "sweep-running"
	case evtGuardSweepUnstamped:
		return "sweep-unstamped"
	default:
		return "release"
	}
}

// guardMachine is the reattach-stall protocol: Begin acquires and stamps; a
// sweep releases only a stamped lock older than ReattachStallTimeout, and
// never one whose item is still running; an unstamped flag gets stamped
// instead of released. Callers also release directly when the outcome lands
// (evtGuardRelease — the tab/terminal code clearing InFlight by hand).
var guardMachine = fsm.MustCompile(fsm.Machine[guardState, guardEvent]{
	Name:    "reattach-guard",
	Initial: guardIdle,
	Steps: []fsm.Step[guardState, guardEvent]{
		{From: guardIdle, Event: evtGuardBegin, To: guardHeldStamped},
		// The defensive anomaly: a flag set outside Begin carries no stamp.
		{From: guardIdle, Event: evtFlagSetWithoutBegin, To: guardHeldUnstamped},
		{From: guardIdle, Event: evtGuardSweepFresh, To: guardIdle},
		{From: guardIdle, Event: evtGuardSweepExpired, To: guardIdle},
		{From: guardIdle, Event: evtGuardSweepRunning, To: guardIdle},
		{From: guardIdle, Event: evtGuardSweepUnstamped, To: guardIdle},
		{From: guardIdle, Event: evtGuardRelease, To: guardIdle},
		{From: guardHeldStamped, Event: evtGuardBegin, To: guardHeldStamped},
		{From: guardHeldStamped, Event: evtGuardSweepFresh, To: guardHeldStamped},
		{From: guardHeldStamped, Event: evtGuardSweepRunning, To: guardHeldStamped},
		{From: guardHeldStamped, Event: evtGuardSweepExpired, To: guardIdle},
		{From: guardHeldStamped, Event: evtGuardRelease, To: guardIdle},
		// A re-begin over a lingering flag re-stamps the hold.
		{From: guardHeldUnstamped, Event: evtGuardBegin, To: guardHeldStamped},
		// Any sweep of an unstamped hold stamps it (running keeps waiting).
		{From: guardHeldUnstamped, Event: evtGuardSweepUnstamped, To: guardHeldStamped},
		{From: guardHeldUnstamped, Event: evtGuardSweepRunning, To: guardHeldUnstamped},
		{From: guardHeldUnstamped, Event: evtGuardRelease, To: guardIdle},
	},
})

// state derives the guard's protocol state from its fields.
func (g *ReattachGuard) state() guardState {
	switch {
	case !g.InFlight:
		return guardIdle
	case g.StartedAt.IsZero():
		return guardHeldUnstamped
	default:
		return guardHeldStamped
	}
}

// GuardCheck formally verifies the guard protocol: every state reachable, a
// release happens only from a held state, an expired stamped lock is the
// only sweep-driven release, and the unstamped anomaly is always repaired
// into a stamped hold (never released blind).
func GuardCheck() error {
	return guardMachine.Check(fsm.Checker[guardState, guardEvent]{
		Alphabet: []guardEvent{
			evtGuardBegin, evtFlagSetWithoutBegin, evtGuardSweepFresh,
			evtGuardSweepExpired, evtGuardSweepRunning, evtGuardSweepUnstamped,
			evtGuardRelease,
		},
		AllDeclaredReachable: true,
		Edge: func(from guardState, event guardEvent, to guardState) error {
			if to == guardIdle && from != guardIdle && event != evtGuardRelease && event != evtGuardSweepExpired {
				return fmt.Errorf("only an explicit release or an expired stamped lock may clear the hold (got %v from %v)", event, from)
			}
			if from == guardHeldUnstamped && to == guardIdle && event == evtGuardSweepUnstamped {
				return errors.New("an unstamped hold must be stamped, not released")
			}
			return nil
		},
	})
}
