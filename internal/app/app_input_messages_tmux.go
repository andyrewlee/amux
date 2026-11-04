package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// Concurrency safety: takes a snapshot of ws.OpenTabs in the Update loop before
// spawning the Cmd. Results return as messages processed where mutations are safe.
func (a *App) syncWorkspaceTabsFromTmux(ws *data.Workspace) tea.Cmd {
	if ws == nil || len(ws.OpenTabs) == 0 {
		return nil
	}
	if !a.tmuxAvailable {
		return nil // Error shown on startup, don't repeat
	}
	// Mutate workspace state on the Bubble Tea update goroutine only.
	wsID := string(ws.ID())
	tabsSnapshot := make([]data.TabInfo, len(ws.OpenTabs))
	copy(tabsSnapshot, ws.OpenTabs)
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxTabsSyncResult{WorkspaceID: wsID}
		}
		allStates, err := svc.AllSessionStates(opts)
		if err != nil {
			// Skip reconciliation entirely when tmux is unresponsive.
			// Preserves current tab states; next tick recovers normally.
			return tmuxTabsSyncResult{WorkspaceID: wsID}
		}

		var updates []tmuxTabStatusUpdate
		for _, tab := range tabsSnapshot {
			if tab.SessionName == "" {
				continue
			}
			state := allStates[tab.SessionName]
			live := state.Exists && state.HasLivePane
			current := data.ParseSessionStatus(string(tab.Status))
			// Detached is attach-ownership: a live session stays detached
			// (no update); only death moves it. Every other status runs
			// through the checked session-status table.
			if current == data.SessionDetached && live {
				continue
			}
			event := data.SessionObservedDead
			if live {
				event = data.SessionObservedLive
			}
			next, ok := data.NextSessionStatus(current, event)
			if !ok || next == current {
				continue
			}
			updates = append(updates, tmuxTabStatusUpdate{
				SessionName:   tab.SessionName,
				Status:        string(next),
				NotifyStopped: next == data.SessionStopped,
			})
		}
		return tmuxTabsSyncResult{
			WorkspaceID: wsID,
			Updates:     updates,
		}
	}
}

type tmuxTabStatusUpdate struct {
	SessionName   string
	Status        string
	NotifyStopped bool
}

type tmuxTabsSyncResult struct {
	WorkspaceID string
	Updates     []tmuxTabStatusUpdate
}
