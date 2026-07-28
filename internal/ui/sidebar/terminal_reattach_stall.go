package sidebar

import (
	"time"

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
func (ts *TerminalState) beginReattachLocked() {
	ts.reattachInFlight = true
	ts.reattachStartedAt = time.Now()
}

// SweepStalledReattaches releases reattach locks whose outcome never arrived.
//
// It is a periodic scan of state rather than a timer armed by each attach path
// so that any cause — and any path added later — is covered without having to
// opt in. It is not a cancellation: the attach goroutine is untouched, and
// releasing the lock simply lets the next attach sweep retry the terminal,
// which is how a sidebar terminal recovers on its own.
func (m *TerminalModel) SweepStalledReattaches() {
	now := time.Now()
	for _, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.State == nil {
				continue
			}
			ts := tab.State
			ts.mu.Lock()
			stalled := false
			switch {
			case !ts.reattachInFlight || ts.Running:
			case ts.reattachStartedAt.IsZero():
				// Never timed (set outside beginReattachLocked): start its clock
				// now rather than releasing an attach that may be in progress.
				ts.reattachStartedAt = now
			case now.Sub(ts.reattachStartedAt) > ptyio.ReattachStallTimeout:
				ts.reattachInFlight = false
				stalled = true
			}
			ts.mu.Unlock()
			if stalled {
				logging.Warn("Sidebar terminal attach for tab %s produced no outcome within %s; releasing reattach lock", tab.ID, ptyio.ReattachStallTimeout)
			}
		}
	}
}
