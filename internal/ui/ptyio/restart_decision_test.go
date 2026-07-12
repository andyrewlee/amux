package ptyio

import (
	"testing"
	"time"
)

func TestDecidePTYRestartLockedUnderLimit(t *testing.T) {
	st := &State{}
	last := time.Duration(-1)
	for i := 0; i < 3; i++ {
		restart, backoff := st.DecidePTYRestartLocked(true, time.Minute, 3)
		if !restart {
			t.Fatalf("call %d: expected restart==true under the limit", i+1)
		}
		if backoff <= 0 {
			t.Fatalf("call %d: expected positive backoff, got %v", i+1, backoff)
		}
		if backoff < last {
			t.Fatalf("call %d: expected backoff to grow or hold, got %v after %v", i+1, backoff, last)
		}
		last = backoff
	}
}

func TestDecidePTYRestartLockedPastLimitDetaches(t *testing.T) {
	st := &State{}
	for i := 0; i < 3; i++ {
		if restart, _ := st.DecidePTYRestartLocked(true, time.Minute, 3); !restart {
			t.Fatalf("call %d: expected restart==true while under the limit", i+1)
		}
	}
	restart, backoff := st.DecidePTYRestartLocked(true, time.Minute, 3)
	if restart {
		t.Fatal("expected restart==false once the budget is exhausted")
	}
	if backoff != 0 {
		t.Fatalf("expected zero backoff past the limit, got %v", backoff)
	}
	if st.RestartBackoff != 0 {
		t.Fatalf("expected RestartBackoff zeroed past the limit, got %v", st.RestartBackoff)
	}
	// The window bookkeeping is intentionally kept (matches the historical
	// pane behavior): only the dead-terminal branch fully resets.
	if st.RestartCount == 0 || st.RestartSince.IsZero() {
		t.Fatal("expected the restart window bookkeeping to survive a limit-reached decision")
	}
}

func TestDecidePTYRestartLockedDeadTerminalResets(t *testing.T) {
	st := &State{}
	// Seed some backoff state first.
	if restart, _ := st.DecidePTYRestartLocked(true, time.Minute, 3); !restart {
		t.Fatal("seed: expected restart==true")
	}
	restart, backoff := st.DecidePTYRestartLocked(false, time.Minute, 3)
	if restart {
		t.Fatal("expected restart==false for a dead terminal")
	}
	if backoff != 0 {
		t.Fatalf("expected zero backoff for a dead terminal, got %v", backoff)
	}
	if st.RestartBackoff != 0 || st.RestartCount != 0 || !st.RestartSince.IsZero() {
		t.Fatalf("expected full backoff reset for a dead terminal, got backoff=%v count=%d since=%v",
			st.RestartBackoff, st.RestartCount, st.RestartSince)
	}
}
