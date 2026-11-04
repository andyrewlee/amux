package ptyio

import (
	"testing"
	"time"
)

// TestRestartPolicyFormallyChecked verifies the reader-restart policy table
// by exhaustive model checking.
func TestRestartPolicyFormallyChecked(t *testing.T) {
	if err := RestartPolicyCheck(); err != nil {
		t.Fatalf("restart policy machine failed model checking: %v", err)
	}
}

// TestRestartBudgetArithmeticMatchesPolicy differentially pins the numeric
// budget implementation (NextRestartBackoffLocked) to the modeled policy:
// budget is spent only while restarts apply, delays double up to the cap,
// exhaustion refuses, and an aged window resets the budget.
func TestRestartBudgetArithmeticMatchesPolicy(t *testing.T) {
	const window = time.Minute
	const maxRestarts = 3

	t.Run("delays double up to the cap", func(t *testing.T) {
		const generousBudget = 10
		st := &State{}
		first, ok := st.NextRestartBackoffLocked(window, generousBudget)
		if !ok || first != RestartBackoffInitial {
			t.Fatalf("first delay = %v ok=%v, want %v", first, ok, RestartBackoffInitial)
		}
		second, _ := st.NextRestartBackoffLocked(window, generousBudget)
		if second != 2*RestartBackoffInitial {
			t.Fatalf("second delay = %v, want %v", second, 2*RestartBackoffInitial)
		}
		// Keep restarting until the delay is capped (200ms doubling reaches
		// the 5s cap on the 6th restart; the generous budget covers it).
		capped := false
		for i := 0; i < generousBudget; i++ {
			if d, ok := st.NextRestartBackoffLocked(window, generousBudget); ok && d == RestartBackoffCap {
				capped = true
				break
			}
		}
		if !capped {
			t.Fatal("delay never reached the cap within the budget")
		}
	})

	t.Run("budget exhausts then refuses and clears backoff", func(t *testing.T) {
		st := &State{}
		for i := 0; i < maxRestarts; i++ {
			if _, ok := st.NextRestartBackoffLocked(window, maxRestarts); !ok {
				t.Fatalf("restart %d refused early", i+1)
			}
		}
		delay, ok := st.NextRestartBackoffLocked(window, maxRestarts)
		if ok {
			t.Fatal("budget must exhaust after maxRestarts within the window")
		}
		if delay != 0 || st.RestartBackoff != 0 {
			t.Fatalf("exhaustion must zero the backoff (delay=%v backoff=%v)", delay, st.RestartBackoff)
		}
	})

	t.Run("aged window resets the budget but not the delay", func(t *testing.T) {
		st := &State{}
		var lastDelay time.Duration
		for i := 0; i < maxRestarts; i++ {
			lastDelay, _ = st.NextRestartBackoffLocked(window, maxRestarts)
		}
		// Simulate the window passing: the next call sees an aged RestartSince.
		st.RestartSince = time.Now().Add(-2 * window)
		delay, ok := st.NextRestartBackoffLocked(window, maxRestarts)
		if !ok || st.RestartCount != 1 {
			t.Fatalf("aged window must reset the budget: ok=%v count=%d", ok, st.RestartCount)
		}
		// The delay is deliberately NOT reset: only StartReader (a fresh
		// reader) and ResetRestartBackoffLocked (detach) clear it, so a
		// reader that keeps dying does not get cheap retries every window.
		want := lastDelay * 2
		if want > RestartBackoffCap {
			want = RestartBackoffCap
		}
		if delay != want {
			t.Fatalf("delay must keep doubling across an aged window: got %v, want %v", delay, want)
		}
	})

	t.Run("dead terminal resets restart state", func(t *testing.T) {
		st := &State{}
		st.NextRestartBackoffLocked(window, maxRestarts)
		restart, _ := st.DecidePTYRestartLocked(false, window, maxRestarts)
		if restart {
			t.Fatal("a dead terminal must not schedule a restart")
		}
		if st.RestartBackoff != 0 || st.RestartCount != 0 || !st.RestartSince.IsZero() {
			t.Fatal("a dead terminal must fully reset the restart state")
		}
	})
}
