package center

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// paneStateProbeFreshness is how long a probed pane screen state stays
// authoritative for input routing. A pane's screen changes rarely (agents
// enter the alternate screen at startup and stay there), so probing on every
// wheel event would be waste; the cache only needs to be fresh enough that a
// pane which did switch screens is re-read on the next interaction.
const paneStateProbeFreshness = 2 * time.Second

// probePaneAlternateScreen reads the pane's authoritative screen state from
// tmux (`alternate_on`) and records it on the terminal. It must run on the
// tab actor, never the UI goroutine: a busy tmux server can make a probe
// take hundreds of milliseconds, and the mouse filter already throttles
// wheel events, so this is at most one round-trip per freshness window.
func (m *Model) probePaneAlternateScreen(tab *Tab) {
	if tab == nil {
		return
	}
	sessionName := withMuVal(&tab.mu, func() string { return tab.SessionName })
	if sessionName == "" {
		return
	}
	probe, err := ptyio.ProbeSessionFn(sessionName, tmux.DefaultOptions())
	if err != nil || !probe.Exists {
		return
	}
	mode := probe.PaneMeta.ModeState
	if !mode.HasState {
		return
	}
	withMu(&tab.mu, func() {
		if tab.Terminal != nil {
			tab.Terminal.SetPaneAlternateScreen(mode.AltScreen)
			tab.paneStateProbedAt = time.Now()
		}
	})
}

// paneStateIsStaleLocked reports whether the cached pane screen state needs a
// probe before an input-routing decision. Caller holds tab.mu.
func (tab *Tab) paneStateIsStaleLocked() bool {
	return time.Since(tab.paneStateProbedAt) > paneStateProbeFreshness
}

// markPaneStateFreshLocked stamps the cache after a state source that is
// itself authoritative (a pane capture's mode state) refreshed the flag.
// Caller holds tab.mu, or is a construction path where the tab is not yet
// shared with any other goroutine.
func (tab *Tab) markPaneStateFreshLocked() {
	tab.paneStateProbedAt = time.Now()
}
