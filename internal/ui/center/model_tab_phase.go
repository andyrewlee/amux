package center

// A Tab's PTY lifecycle is held as the underlying flags
// (Running/Detached/reattachInFlight) rather than a derived phase value:
// Running and Detached are exported package API (the app, harness and
// persistence read them), so the flags remain the storage while every
// multi-field transition goes through the methods below. That keeps the
// implicit invariants — e.g. "a detached tab is never Running", "a stopped
// reattach must clear the in-flight guard" — in one place.
//
// Legal transitions:
//
//	stopped     → running        (markAttachedLocked: tab created/agent launched)
//	running     → detached       (markDetachedLocked: PTY lost, session may live)
//	running     → stopped        (markStoppedLocked: agent closed for good)
//	detached    → reattaching    (beginReattachLocked)
//	reattaching → running        (markAttachedLocked: reattach succeeded)
//	reattaching → detached       (markDetachedEndingReattachLocked / reattach failed)
//	reattaching → stopped        (markReattachFailedLocked(stopped=true))

// markAttachedLocked transitions to running: the tab has a live PTY (fresh
// launch or successful reattach). Clears any reattach lock.
func (t *Tab) markAttachedLocked() {
	t.Detached = false
	t.reattachInFlight = false
	t.Running = true
}

// markDetachedLocked transitions to detached: the PTY is gone but the tmux
// session may still be alive, so the tab is offered for reattach. It does not
// touch the reattach lock — restart/input-failure paths may run while a
// reattach is in flight, and the reattach outcome owns that flag.
func (t *Tab) markDetachedLocked() {
	t.Running = false
	t.Detached = true
}

// markDetachedEndingReattachLocked transitions to detached and releases the
// reattach lock; used by the session-detach path, which is itself the
// terminal outcome of any in-flight reattach.
func (t *Tab) markDetachedEndingReattachLocked() {
	t.Running = false
	t.Detached = true
	t.reattachInFlight = false
}

// markStoppedLocked transitions to stopped: no PTY and no session worth
// reattaching. Clears the in-flight reattach guard too: this is the only
// stop/detach transition that previously did not, leaving a tab wedged if a
// stopped message landed while a reattach was in flight (all reattach gates
// bail on this flag, so the user could no longer reattach a tab that now
// shows stopped).
func (t *Tab) markStoppedLocked() {
	t.Running = false
	t.Detached = false
	t.reattachInFlight = false
}

// markReattachFailedLocked records a failed reattach: the tab is no longer
// running and the lock is released. A stopped outcome also clears Detached so
// the tab shows as stopped rather than detached.
func (t *Tab) markReattachFailedLocked(stopped bool) {
	t.Running = false
	t.reattachInFlight = false
	if stopped {
		t.Detached = false
	}
}

// beginReattachLocked acquires the reattach transition lock, reporting false
// when a reattach is already in flight.
func (t *Tab) beginReattachLocked() bool {
	if t.reattachInFlight {
		return false
	}
	t.reattachInFlight = true
	return true
}

// endReattachLocked releases the reattach transition lock without changing
// the running/detached outcome (used on early-bail paths).
func (t *Tab) endReattachLocked() {
	t.reattachInFlight = false
}
