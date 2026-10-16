package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// beginReattachLocked acquires the reattach lock and stamps the acquisition.
// The caller must hold ts.mu.
//
// The stamp exists because the lock is released only when the attach outcome
// comes back. An outcome that is dropped, misrouted, or never produced would
// otherwise leave the terminal unable to reattach for the rest of the process
// lifetime: shouldAttachExistingTerminalTab refuses every retry while this
// flag is set. The stamp is what lets the sweep tell a slow attach from a lost
// one; see SweepStalledReattaches.
//
// The epoch bump exists because the sweep makes retries possible while an
// earlier attempt may still be running: without it, a slow attempt returning
// after the retry would overwrite the newer attempt's terminal with its own —
// leaking a tmux client and pointing the tab at a PTY the retry superseded.
// Results carry the epoch they were dispatched under and are dropped when the
// epoch no longer names the current in-flight attempt.
func (ts *TerminalState) beginReattachLocked() bool {
	if ts.Reattach.InFlight {
		return false
	}
	ts.Reattach.Begin()
	ts.reattachEpoch++
	return true
}

// reattachAttemptCurrentLocked reports whether epoch names the still-current
// in-flight attempt. An outcome is current only while the attempt that
// produced it still holds the lock: detach, teardown, stall release, and an
// already-accepted outcome all make it stale.
func (ts *TerminalState) reattachAttemptCurrentLocked(epoch uint64) bool {
	return ts.Reattach.InFlight && epoch == ts.reattachEpoch
}

// finishReattachLocked releases the attempt's lock on an accepted outcome.
// The epoch is left unchanged, so reattachAttemptCurrentLocked stays false
// for a duplicate delivery of the same result — InFlight no longer holds.
func (ts *TerminalState) finishReattachLocked() {
	ts.Reattach.InFlight = false
}

// invalidateReattachLocked ends the current attempt without an outcome:
// the epoch advances so a late result is rejected by
// reattachAttemptCurrentLocked, and the lock frees so a new attempt can
// begin immediately. Used by explicit detach, teardown, and the stall sweep.
func (ts *TerminalState) invalidateReattachLocked() {
	ts.reattachEpoch++
	ts.Reattach.InFlight = false
}

// SweepStalledReattaches releases reattach locks whose outcome never arrived.
//
// It is a periodic scan of state rather than a timer armed by each attach path
// so that any cause — and any path added later — is covered without having to
// opt in. It is not a cancellation: the attach goroutine is untouched, and
// releasing the lock simply lets the next attach sweep retry the terminal,
// which is how a sidebar terminal recovers on its own.
func (m *TerminalModel) SweepStalledReattaches() tea.Cmd {
	now := time.Now()
	for _, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.State == nil {
				continue
			}
			ts := tab.State
			stalled := withMuVal(&ts.mu, func() bool {
				if ts.Reattach.Sweep(now, ts.Running) {
					// Invalidate the expired attempt's epoch, not just its lock:
					// the abandoned attach may still complete, and its result
					// must be rejected even before the retry begins.
					ts.invalidateReattachLocked()
					return true
				}
				return false
			})
			if stalled {
				m.stalledReattachReleases++
				logging.Warn("Sidebar terminal attach for tab %s produced no outcome within %s; releasing reattach lock", tab.ID, ptyio.ReattachStallTimeout)
			}
		}
	}
	// Unlike the center sweep there is nothing to dispatch: a stalled sidebar
	// terminal recovers on its own the next time the attach sweep retries it.
	return nil
}

// StalledReattachReleases reports how many locks the stall sweep has
// released — the only side effect visible outside the unexported tabs map.
// Test instrumentation for the app watchdog's wiring assertions; not for
// production use.
func (m *TerminalModel) StalledReattachReleases() int {
	return m.stalledReattachReleases
}

// StallReattachForTest pins the workspace's first terminal tab in the
// stalled-reattach state SweepStalledReattaches exists to release. The lock
// is stamped far enough in the past to qualify on the next sweep. Test-only
// — production reaches this state only through a dropped reattach outcome.
func (m *TerminalModel) StallReattachForTest(ws *data.Workspace) {
	if ws == nil {
		return
	}
	for _, tab := range m.tabs.ByWorkspace[string(ws.ID())] {
		if tab == nil || tab.State == nil {
			continue
		}
		ts := tab.State
		withMu(&ts.mu, func() {
			ts.Running = false
			ts.Reattach.InFlight = true
			ts.Reattach.StartedAt = time.Now().Add(-2 * ptyio.ReattachStallTimeout)
			ts.reattachEpoch++
		})
		return
	}
}
