package center

import "testing"

func TestTabPTYPhaseTransitions(t *testing.T) {
	tab := &Tab{}
	if got := tab.ptyPhaseLocked(); got != ptyPhaseStopped {
		t.Fatalf("new tab phase = %s, want stopped", got)
	}

	tab.markAttachedLocked()
	if got := tab.ptyPhaseLocked(); got != ptyPhaseRunning {
		t.Fatalf("after attach phase = %s, want running", got)
	}

	tab.markDetachedLocked()
	if got := tab.ptyPhaseLocked(); got != ptyPhaseDetached {
		t.Fatalf("after detach phase = %s, want detached", got)
	}

	if !tab.beginReattachLocked() {
		t.Fatal("expected reattach lock acquired")
	}
	if got := tab.ptyPhaseLocked(); got != ptyPhaseReattaching {
		t.Fatalf("during reattach phase = %s, want reattaching", got)
	}
	if tab.beginReattachLocked() {
		t.Fatal("expected second reattach to be rejected while one is in flight")
	}

	// Failed reattach with a stopped outcome settles to stopped, not detached.
	tab.markReattachFailedLocked(true)
	if got := tab.ptyPhaseLocked(); got != ptyPhaseStopped {
		t.Fatalf("after stopped reattach failure phase = %s, want stopped", got)
	}

	// Failed reattach without a stopped outcome returns to detached.
	tab.markDetachedLocked()
	_ = tab.beginReattachLocked()
	tab.markReattachFailedLocked(false)
	if got := tab.ptyPhaseLocked(); got != ptyPhaseDetached {
		t.Fatalf("after reattach failure phase = %s, want detached", got)
	}

	// A stop while a reattach is in flight must release the lock so the tab
	// cannot wedge in a state where no reattach gate will ever pass.
	_ = tab.beginReattachLocked()
	tab.markStoppedLocked()
	if got := tab.ptyPhaseLocked(); got != ptyPhaseStopped {
		t.Fatalf("after stop phase = %s, want stopped", got)
	}
	if !tab.beginReattachLocked() {
		t.Fatal("expected reattach available again after stop")
	}
}
