// Package sidebar: terminal_tab_model.go holds the tab lifecycle machine —
// the explicit replacement for the former Running/Detached/UserDetached bool
// trio on TerminalState.
package sidebar

import (
	"errors"
	"fmt"
	"slices"

	"github.com/andyrewlee/amux/internal/fsm"
)

// tabState is a terminal tab's lifecycle state. It collapses the former
// Running/Detached/UserDetached bool trio, whose eight combinations admitted
// illegal-by-accident states (Running∧Detached, UserDetached∧¬Detached,
// Running∧reattach-in-flight); the enum makes those unrepresentable.
//
// The PTY pointer, VTerm, reattach guard, and epoch stay alongside as
// extended state — the machine governs control flow only. Guarded by mu.
type tabState uint8

const (
	// tabStopped is the zero value: no live client and no live-session
	// knowledge — a reattach failed because the session ended, or the tab
	// was constructed bare. Restart- and attach-eligible.
	tabStopped tabState = iota
	// tabRunning: a live attached PTY client is streaming.
	tabRunning
	// tabAttaching: a reattach attempt is in flight (Reattach.InFlight,
	// stamped with reattachEpoch). Invariant: tabAttaching holds exactly
	// while Reattach.InFlight — every guard mutation steps the machine in
	// the same ts.mu hold.
	tabAttaching
	// tabDetachedUser: the user detached on purpose; the tmux session stays
	// alive and auto-reattach must not reverse the decision.
	tabDetachedUser
	// tabDetachedAuto: detached without user consent — PTY death past the
	// restart limit, attached-tab-limit enforcement, input-writer failure,
	// or a session-discovery tab that never attached. Auto-reattach
	// eligible.
	tabDetachedAuto
	// tabTornDown: teardownTabState ran and the tab is being discarded; it
	// must never accept another attempt. The machine's terminal state.
	tabTornDown
)

func (s tabState) String() string {
	switch s {
	case tabRunning:
		return "running"
	case tabAttaching:
		return "attaching"
	case tabDetachedUser:
		return "detached-user"
	case tabDetachedAuto:
		return "detached-auto"
	case tabTornDown:
		return "torn-down"
	default:
		return "stopped"
	}
}

// tabEvent is the tab lifecycle alphabet. Every producer site reduces to
// exactly one of these; epochs, timestamps, and PTY pointers are extended
// state carried by the events' call sites.
type tabEvent uint8

const (
	evDetachUser        tabEvent = iota // user-initiated detach
	evDetachAuto                        // automatic detach: PTY give-up, LRU limit, input failure
	evBeginReattach                     // a reattach attempt acquires the guard
	evReattachSucceeded                 // the current attempt returned a live client
	evReattachFailed                    // the current attempt failed, retryable (incl. no-terminal)
	evReattachDead                      // the current attempt failed: the session ended
	evReattachStalled                   // the stall sweep released a lost attempt
	evTeardown                          // the tab is being discarded
)

func (e tabEvent) String() string {
	switch e {
	case evDetachUser:
		return "detach-user"
	case evDetachAuto:
		return "detach-auto"
	case evBeginReattach:
		return "begin-reattach"
	case evReattachSucceeded:
		return "reattach-succeeded"
	case evReattachFailed:
		return "reattach-failed"
	case evReattachDead:
		return "reattach-dead"
	case evReattachStalled:
		return "reattach-stalled"
	default:
		return "teardown"
	}
}

// tabMachine is the terminal tab lifecycle table: a tab detaches with the
// user-vs-auto cause recorded in the landing state, reattach attempts begin
// only from a not-running state (so Running can never coexist with an
// in-flight attempt), outcomes land only from tabAttaching, and teardown is
// final. Rejected (state, event) pairs — begin from running, anything from
// torn-down, outcomes from a non-attaching tab — are the fail-closed guards
// the call sites rely on.
var tabMachine = fsm.MustCompile(fsm.Machine[tabState, tabEvent]{
	Name:    "sidebar-terminal-tab",
	Initial: tabStopped,
	Steps: []fsm.Step[tabState, tabEvent]{
		{From: tabRunning, Event: evDetachUser, To: tabDetachedUser},
		{From: tabRunning, Event: evDetachAuto, To: tabDetachedAuto},
		{From: tabRunning, Event: evTeardown, To: tabTornDown},

		{From: tabAttaching, Event: evDetachUser, To: tabDetachedUser},
		{From: tabAttaching, Event: evDetachAuto, To: tabDetachedAuto},
		{From: tabAttaching, Event: evReattachSucceeded, To: tabRunning},
		{From: tabAttaching, Event: evReattachFailed, To: tabDetachedAuto},
		{From: tabAttaching, Event: evReattachDead, To: tabStopped},
		{From: tabAttaching, Event: evReattachStalled, To: tabDetachedAuto},
		{From: tabAttaching, Event: evTeardown, To: tabTornDown},

		{From: tabDetachedUser, Event: evDetachUser, To: tabDetachedUser},
		{From: tabDetachedUser, Event: evDetachAuto, To: tabDetachedAuto},
		{From: tabDetachedUser, Event: evBeginReattach, To: tabAttaching},
		{From: tabDetachedUser, Event: evTeardown, To: tabTornDown},

		{From: tabDetachedAuto, Event: evDetachUser, To: tabDetachedUser},
		{From: tabDetachedAuto, Event: evDetachAuto, To: tabDetachedAuto},
		{From: tabDetachedAuto, Event: evBeginReattach, To: tabAttaching},
		{From: tabDetachedAuto, Event: evTeardown, To: tabTornDown},

		{From: tabStopped, Event: evDetachUser, To: tabDetachedUser},
		{From: tabStopped, Event: evDetachAuto, To: tabDetachedAuto},
		{From: tabStopped, Event: evBeginReattach, To: tabAttaching},
		{From: tabStopped, Event: evTeardown, To: tabTornDown},
	},
	Terminals: []tabState{tabTornDown},
})

// TabLifecycleCheck formally verifies the tab lifecycle table: every state
// reachable, detach always records the user-vs-auto cause, attempts begin
// only from a not-running state, reattach outcomes (including the stall
// sweep's release) apply only from tabAttaching, and teardown is final.
func TabLifecycleCheck() error {
	if err := tabMachine.Check(fsm.Checker[tabState, tabEvent]{
		Alphabet: []tabEvent{
			evDetachUser, evDetachAuto, evBeginReattach, evReattachSucceeded,
			evReattachFailed, evReattachDead, evReattachStalled, evTeardown,
		},
		AllDeclaredReachable: true,
		Edge: func(from tabState, event tabEvent, to tabState) error {
			switch event {
			case evBeginReattach:
				if from == tabRunning || from == tabAttaching || from == tabTornDown {
					return fmt.Errorf("an attempt may begin only from a detached or stopped tab, not %v", from)
				}
			case evReattachSucceeded, evReattachFailed, evReattachDead, evReattachStalled:
				if from != tabAttaching {
					return fmt.Errorf("an attempt outcome (%v) applies only to an in-flight attempt, not %v", event, from)
				}
			case evDetachUser:
				if to != tabDetachedUser {
					return errors.New("a user detach must record the user cause")
				}
			case evDetachAuto:
				if to != tabDetachedAuto {
					return errors.New("an automatic detach must record the automatic cause")
				}
			case evTeardown:
				if to != tabTornDown {
					return errors.New("teardown must be final")
				}
			}
			return nil
		},
	}); err != nil {
		return err
	}
	// Every live tab can always recover to running (reattach after a
	// give-up) and always be torn down — the two liveness obligations the
	// render/reattach sweeps depend on.
	for _, st := range []tabState{tabRunning, tabAttaching, tabDetachedUser, tabDetachedAuto, tabStopped} {
		reachable := tabMachine.ReachableFrom(st, nil)
		if !slices.Contains(reachable, tabRunning) {
			return fmt.Errorf("state %v cannot reach running: a detached or attaching tab must always be reattachable", st)
		}
		if !slices.Contains(reachable, tabTornDown) {
			return fmt.Errorf("state %v cannot reach torn-down: every tab must always be closable", st)
		}
	}
	return nil
}

// stepTabLocked advances the tab lifecycle machine. The caller must hold
// ts.mu. ok=false means the table rejects the move — a fail-closed no-op,
// never a crash (see the fsm package contract).
func (ts *TerminalState) stepTabLocked(ev tabEvent) (tabState, bool) {
	next, ok := tabMachine.Next(ts.tab, ev)
	if ok {
		ts.tab = next
	}
	return next, ok
}

// reattachOutcomeLocked applies an attempt outcome and releases the reattach
// guard in the same step, keeping the tabAttaching ⇔ Reattach.InFlight
// invariant true by construction. The epoch check stays at the caller: an
// outcome from a superseded attempt must not step anything. Caller holds
// ts.mu; returns false when the tab is not mid-attempt (duplicate delivery).
func (ts *TerminalState) reattachOutcomeLocked(ev tabEvent) bool {
	if ts.tab != tabAttaching {
		return false
	}
	if _, ok := ts.stepTabLocked(ev); !ok {
		return false
	}
	ts.Reattach.InFlight = false
	return true
}
