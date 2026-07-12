package center

import "testing"

func TestTabPTYPhaseTransitions(t *testing.T) {
	tab := &Tab{}

	assertPhase := func(label string, wantRunning, wantDetached, wantReattaching bool) {
		t.Helper()
		if tab.Running != wantRunning || tab.Detached != wantDetached || tab.reattachInFlight != wantReattaching {
			t.Fatalf("%s: Running=%v Detached=%v reattachInFlight=%v, want Running=%v Detached=%v reattachInFlight=%v",
				label, tab.Running, tab.Detached, tab.reattachInFlight, wantRunning, wantDetached, wantReattaching)
		}
	}

	assertPhase("new tab", false, false, false)

	tab.markAttachedLocked()
	assertPhase("after attach", true, false, false)

	tab.markDetachedLocked()
	assertPhase("after detach", false, true, false)

	if !tab.beginReattachLocked() {
		t.Fatal("expected reattach lock acquired")
	}
	assertPhase("during reattach", false, true, true)
	if tab.beginReattachLocked() {
		t.Fatal("expected second reattach to be rejected while one is in flight")
	}

	// Failed reattach with a stopped outcome settles to stopped, not detached.
	tab.markReattachFailedLocked(true)
	assertPhase("after stopped reattach failure", false, false, false)

	// Failed reattach without a stopped outcome returns to detached.
	tab.markDetachedLocked()
	_ = tab.beginReattachLocked()
	tab.markReattachFailedLocked(false)
	assertPhase("after reattach failure", false, true, false)

	// A stop while a reattach is in flight must release the lock so the tab
	// cannot wedge in a state where no reattach gate will ever pass.
	_ = tab.beginReattachLocked()
	tab.markStoppedLocked()
	assertPhase("after stop", false, false, false)
	if !tab.beginReattachLocked() {
		t.Fatal("expected reattach available again after stop")
	}
}
