package process

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// coordState is a workspace key's position in the lifecycle admission
// protocol, derived from the coordinator's observed fields (the lifecycleState
// record is the extended state; the derivation below is the single mapping
// between those fields and the protocol machine). A key with no record is
// idle: stateFor recreates an identical zero-value record on demand, so
// absence and an empty record are the same protocol state.
type coordState uint8

const (
	// coordIdle: no teardown gate, no in-flight setup sequence.
	coordIdle coordState = iota
	// coordSetupInflight: a setup ticket holds the current generation and
	// the slot; its commands may still start.
	coordSetupInflight
	// coordSetupStale: a ticket still occupies the setup slot but its
	// generation was superseded (a teardown began and released); nothing
	// of it may start, and the slot stays busy until finishSetup runs.
	coordSetupStale
	// coordTeardownGated: the teardown gate is held with no ticket in the
	// slot; admission is refused until the guard finishes.
	coordTeardownGated
	// coordTeardownGatedStale: the gate is held over a superseded ticket
	// that teardown is draining.
	coordTeardownGatedStale
)

func (s coordState) String() string {
	switch s {
	case coordSetupInflight:
		return "setup-inflight"
	case coordSetupStale:
		return "setup-stale"
	case coordTeardownGated:
		return "teardown-gated"
	case coordTeardownGatedStale:
		return "teardown-gated-stale"
	default:
		return "idle"
	}
}

// coordEvent is the lifecycle admission protocol's alphabet. Rejected
// (state, event) pairs carry no table edge — matching the coordinator's
// fail-closed guards — except where acceptance itself is the observable
// (evtTicketCurrent), which is a self-loop edge so the checker can pin it
// to exactly one state.
type coordEvent uint8

const (
	evtAdmitSetup coordEvent = iota
	evtFinishSetup
	evtTicketCurrent
	evtBeginTeardown
	evtReleaseTeardown
	evtFinishRemoved
	evtForget
	evtAdmitOnDone
	evtFinishOnDone
	evtStopAll
)

func (e coordEvent) String() string {
	names := []string{
		"admit-setup", "finish-setup", "ticket-current", "begin-teardown",
		"release-teardown", "finish-removed", "forget", "admit-on-done",
		"finish-on-done", "stop-all",
	}
	if int(e) < len(names) {
		return names[e]
	}
	return fmt.Sprintf("coord-event-%d", uint8(e))
}

// coordMachine is the per-key lifecycle admission table: idle admits a setup
// sequence or a teardown gate; a teardown supersedes the in-flight ticket's
// generation (making every later ticket check refuse) and holds admission
// until the guard releases (gate drops, bumped generation kept) or the
// workspace is removed (record deleted). The global stopped latch is
// orthogonal: evtStopAll returns every key to idle and, at the call site,
// permanently refuses further admission — that latch is exercised by the
// differential test, not by this table.
var coordMachine = fsm.MustCompile(fsm.Machine[coordState, coordEvent]{
	Name:    "lifecycle-coordinator",
	Initial: coordIdle,
	Steps: []fsm.Step[coordState, coordEvent]{
		{From: coordIdle, Event: evtAdmitSetup, To: coordSetupInflight},
		{From: coordIdle, Event: evtAdmitOnDone, To: coordIdle},
		{From: coordIdle, Event: evtFinishOnDone, To: coordIdle},
		{From: coordIdle, Event: evtFinishSetup, To: coordIdle},
		{From: coordIdle, Event: evtBeginTeardown, To: coordTeardownGated},
		{From: coordIdle, Event: evtForget, To: coordIdle},
		{From: coordIdle, Event: evtStopAll, To: coordIdle},

		{From: coordSetupInflight, Event: evtFinishSetup, To: coordIdle},
		// Acceptance self-loop: only a current ticket passes revalidation.
		{From: coordSetupInflight, Event: evtTicketCurrent, To: coordSetupInflight},
		{From: coordSetupInflight, Event: evtBeginTeardown, To: coordTeardownGatedStale},
		{From: coordSetupInflight, Event: evtAdmitOnDone, To: coordSetupInflight},
		{From: coordSetupInflight, Event: evtFinishOnDone, To: coordSetupInflight},
		{From: coordSetupInflight, Event: evtStopAll, To: coordIdle},

		{From: coordSetupStale, Event: evtFinishSetup, To: coordIdle},
		{From: coordSetupStale, Event: evtBeginTeardown, To: coordTeardownGatedStale},
		{From: coordSetupStale, Event: evtAdmitOnDone, To: coordSetupStale},
		{From: coordSetupStale, Event: evtFinishOnDone, To: coordSetupStale},
		{From: coordSetupStale, Event: evtStopAll, To: coordIdle},

		{From: coordTeardownGated, Event: evtReleaseTeardown, To: coordIdle},
		{From: coordTeardownGated, Event: evtFinishRemoved, To: coordIdle},
		{From: coordTeardownGated, Event: evtStopAll, To: coordIdle},

		{From: coordTeardownGatedStale, Event: evtFinishSetup, To: coordTeardownGated},
		{From: coordTeardownGatedStale, Event: evtReleaseTeardown, To: coordSetupStale},
		{From: coordTeardownGatedStale, Event: evtFinishRemoved, To: coordIdle},
		{From: coordTeardownGatedStale, Event: evtStopAll, To: coordIdle},
	},
})

// deriveCoordState maps a key's observed lifecycleState (or its absence) to
// the protocol state. It is the only place field combos become states.
func deriveCoordState(st *lifecycleState) coordState {
	switch {
	case st == nil:
		return coordIdle
	case st.teardown && st.setup == nil:
		return coordTeardownGated
	case st.teardown:
		return coordTeardownGatedStale
	case st.setup == nil:
		return coordIdle
	case st.setup.gen == st.gen:
		return coordSetupInflight
	default:
		return coordSetupStale
	}
}

// coordStateOf derives the key's protocol state under the coordinator mutex.
func (c *lifecycleCoordinator) coordStateOf(key string) coordState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return deriveCoordState(c.states[key])
}

// coordMachineCheck formally verifies the admission table: every declared
// state reachable; setup admission only from idle; ticket revalidation
// accepted only from setup-inflight (a superseded generation is never
// re-accepted); teardown always lands in a gated state and is sticky — the
// only exits from a gated state are guard release, workspace removal, and
// StopAll; StopAll always returns the key to idle.
func coordMachineCheck() error {
	gated := func(s coordState) bool {
		return s == coordTeardownGated || s == coordTeardownGatedStale
	}
	return coordMachine.Check(fsm.Checker[coordState, coordEvent]{
		Alphabet: []coordEvent{
			evtAdmitSetup, evtFinishSetup, evtTicketCurrent, evtBeginTeardown,
			evtReleaseTeardown, evtFinishRemoved, evtForget, evtAdmitOnDone,
			evtFinishOnDone, evtStopAll,
		},
		AllDeclaredReachable: true,
		Edge: func(from coordState, event coordEvent, to coordState) error {
			switch event {
			case evtAdmitSetup:
				if from != coordIdle || to != coordSetupInflight {
					return fmt.Errorf("setup may be admitted only from idle (got %v --admit-setup--> %v)", from, to)
				}
			case evtTicketCurrent:
				if from != coordSetupInflight || to != coordSetupInflight {
					return fmt.Errorf("only a current in-flight ticket passes revalidation (got %v --ticket-current--> %v)", from, to)
				}
			case evtBeginTeardown:
				if !gated(to) {
					return fmt.Errorf("begin-teardown must land in a gated state, got %v", to)
				}
			case evtReleaseTeardown:
				if to != coordIdle && to != coordSetupStale {
					return fmt.Errorf("release-teardown must open the gate to idle or a lingering stale ticket, got %v", to)
				}
			case evtFinishRemoved, evtStopAll:
				if to != coordIdle {
					return fmt.Errorf("%v must return the key to idle, got %v", event, to)
				}
			}
			if gated(from) && !gated(to) {
				switch event {
				case evtReleaseTeardown, evtFinishRemoved, evtStopAll:
					// The three sanctioned exits from the gate.
				default:
					return fmt.Errorf("teardown is sticky: %v may not leave a gated state (got %v --%v--> %v)", event, from, event, to)
				}
			}
			return nil
		},
	})
}

// coordProgressHolds asserts the drain liveness shape the checker cannot
// express as an edge property: from every state that holds a ticket (current
// or stale), the table still offers finish-setup, and from every non-gated
// state it still offers begin-teardown — there is no reachable dead end
// where a ticket is neither finishable nor teardown-drainable.
func coordProgressHolds() error {
	for _, s := range []coordState{coordSetupInflight, coordSetupStale, coordTeardownGatedStale} {
		if _, ok := coordMachine.Next(s, evtFinishSetup); !ok {
			return errors.New("state holding a ticket must still accept finish-setup: " + s.String())
		}
	}
	for _, s := range []coordState{coordIdle, coordSetupInflight, coordSetupStale} {
		if _, ok := coordMachine.Next(s, evtBeginTeardown); !ok {
			return errors.New("non-gated state must still accept begin-teardown: " + s.String())
		}
	}
	return nil
}
