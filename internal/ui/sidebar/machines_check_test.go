package sidebar

import "testing"

// TestMachinesFormallyChecked verifies every state machine this package
// ships: each table is exhausted over its alphabet, invariants hold on every
// reachable state and edge, and every declared state is reachable. A failure
// here means a table edit broke a documented protocol — the witness path in
// the error names the shortest event sequence that exhibits it.
func TestMachinesFormallyChecked(t *testing.T) {
	checks := []struct {
		name  string
		check func() error
	}{
		{"terminal tab lifecycle", TabLifecycleCheck},
		{"changes async fetch", FetchCheck},
		{"project tree load", TreeLoadCheck},
		{"sidebar input binding", inputBindingCheck},
	}
	for _, tc := range checks {
		if err := tc.check(); err != nil {
			t.Errorf("%s machine failed model checking: %v", tc.name, err)
		}
	}
}

// TestTabMachineRejectsIllegalBegin pins the fail-closed guards directly:
// a running, attaching, or torn-down tab can never begin a reattach attempt,
// and outcomes never apply to a tab that is not mid-attempt.
func TestTabMachineRejectsIllegalBegin(t *testing.T) {
	for _, st := range []tabState{tabRunning, tabAttaching, tabTornDown} {
		if _, ok := tabMachine.Next(st, evBeginReattach); ok {
			t.Errorf("begin-reattach from %v must be rejected", st)
		}
	}
	for _, st := range []tabState{tabStopped, tabDetachedUser, tabDetachedAuto, tabRunning, tabTornDown} {
		for _, ev := range []tabEvent{evReattachSucceeded, evReattachFailed, evReattachDead, evReattachStalled} {
			if _, ok := tabMachine.Next(st, ev); ok {
				t.Errorf("outcome %v from %v must be rejected (outcomes apply only while attaching)", ev, st)
			}
		}
	}
	// Torn-down is terminal: nothing leaves it.
	for _, ev := range tabMachine.Events() {
		if _, ok := tabMachine.Next(tabTornDown, ev); ok {
			t.Errorf("torn-down must be final, but %v is accepted", ev)
		}
	}
}
