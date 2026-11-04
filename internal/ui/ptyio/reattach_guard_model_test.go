package ptyio

import (
	"testing"
	"time"
)

// TestGuardMachineFormallyChecked verifies the reattach-guard protocol table
// by exhaustive model checking.
func TestGuardMachineFormallyChecked(t *testing.T) {
	if err := GuardCheck(); err != nil {
		t.Fatalf("reattach guard machine failed model checking: %v", err)
	}
}

// TestGuardRuntimeMatchesMachine differentially verifies the shipped
// ReattachGuard against the protocol machine: for every reachable state and
// every event, running the real Begin/Sweep/manual-release produces exactly
// the modeled transition.
func TestGuardRuntimeMatchesMachine(t *testing.T) {
	// A held-stamped fixture stamps "now"; the fresh sweep runs at the same
	// instant (age 0), the expired sweep runs an hour later (age >> timeout).
	stamp := time.Now()
	later := stamp.Add(time.Hour)

	// fixture builds a guard in the given protocol state.
	fixture := func(s guardState) ReattachGuard {
		switch s {
		case guardHeldStamped:
			return ReattachGuard{InFlight: true, StartedAt: stamp}
		case guardHeldUnstamped:
			return ReattachGuard{InFlight: true}
		default:
			return ReattachGuard{}
		}
	}

	// run applies the event through the real implementation.
	run := func(g *ReattachGuard, event guardEvent) {
		switch event {
		case evtGuardBegin:
			g.Begin()
		case evtFlagSetWithoutBegin:
			g.InFlight = true // the anomaly the defensive state exists for
		case evtGuardSweepFresh:
			g.Sweep(stamp, false)
		case evtGuardSweepExpired:
			g.Sweep(later, false)
		case evtGuardSweepRunning:
			g.Sweep(later, true) // running outranks even an expired stamp
		case evtGuardSweepUnstamped:
			g.Sweep(stamp, false) // from unstamped: stamps instead of timing
		case evtGuardRelease:
			g.InFlight = false
		}
	}

	for _, state := range []guardState{guardIdle, guardHeldStamped, guardHeldUnstamped} {
		for _, event := range guardMachine.Events() {
			if state == guardHeldUnstamped && (event == evtGuardSweepFresh || event == evtGuardSweepExpired) {
				// Modeled as a single sweep-unstamped event from that state.
				continue
			}
			if state == guardIdle && event == evtGuardSweepUnstamped {
				continue
			}
			modeled, ok := guardMachine.Next(state, event)
			if !ok {
				// Rejected by the table: verify the runtime also leaves the
				// state untouched rather than inventing a transition.
				g := fixture(state)
				run(&g, event)
				if g.state() != state {
					t.Errorf("runtime invented transition %v --%v--> %v (table rejects)", state, event, g.state())
				}
				continue
			}
			g := fixture(state)
			run(&g, event)
			if got := g.state(); got != modeled {
				t.Errorf("runtime mismatch on %v --%v-->: got %v, model says %v", state, event, got, modeled)
			}
		}
	}
}
