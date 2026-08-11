package center

import "time"

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
	t.discardDetachedPTYOutput = false
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
	t.discardDetachedPTYOutput = false
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
//
// The acquisition is stamped because the lock is only released by the reattach
// outcome coming back. An outcome that is dropped, misrouted, or never produced
// would otherwise pin the tab in the reattaching state for the rest of the
// process lifetime — with every retry silently no-oping behind this same lock.
// The stamp is what lets the periodic sweep tell a slow reattach from a lost
// one; see SweepStalledReattaches.
//
// The epoch bump exists because that sweep makes retries possible while an
// earlier attempt may still be running: without it, a slow attempt returning
// after the retry would overwrite the newer attempt's agent with its own —
// leaking a tmux client and pointing the tab at a PTY the retry already
// superseded. Results carry the epoch they were dispatched under and are
// dropped if a newer attempt has since started.
func (t *Tab) beginReattachLocked() bool {
	if t.reattachInFlight {
		return false
	}
	t.reattachInFlight = true
	t.reattachStartedAt = time.Now()
	t.reattachEpoch++
	return true
}

// reattachEpochLocked reports the current reattach generation.
func (t *Tab) reattachEpochLocked() uint64 {
	return t.reattachEpoch
}

// endReattachLocked releases the reattach transition lock without changing
// the running/detached outcome (used on early-bail paths).
func (t *Tab) endReattachLocked() {
	t.reattachInFlight = false
}
