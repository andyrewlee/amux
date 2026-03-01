package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// persistAllWorkspacesNow saves all workspace tab state synchronously.
// Called before shutdown to ensure tabs are persisted before they are closed.
// This intentionally includes delete-in-flight workspaces. If a delete fails or
// races with shutdown, preserving UI tab state is preferred over dropping it.
func (a *App) persistAllWorkspacesNow() {
	if a.workspaceService == nil || a.center == nil {
		return
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			wsID := string(ws.ID())
			tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
			if len(tabs) == 0 && !a.center.HasWorkspaceState(wsID) {
				continue
			}
			ws.OpenTabs = tabs
			ws.ActiveTabIndex = activeIdx
			snap := snapshotWorkspaceForSave(ws)
			if err := a.workspaceService.Save(snap); err != nil {
				logging.Warn("Failed to persist workspace on shutdown: %v", err)
			} else {
				a.markLocalWorkspaceSaveForID(string(snap.ID()))
			}
		}
	}
	// Clear dirty set since we just saved everything
	for k := range a.dirtyWorkspaces {
		delete(a.dirtyWorkspaces, k)
	}
}

// persistDebounceMsg is sent after the debounce period to trigger actual save.
type persistDebounceMsg struct {
	token int
}

// persistWorkspaceTabs marks a workspace dirty and schedules a debounced save.
func (a *App) persistWorkspaceTabs(wsID string) tea.Cmd {
	if wsID == "" {
		return nil
	}
	if a.isWorkspaceDeleteInFlight(wsID) {
		return nil
	}
	if a.dirtyWorkspaces == nil {
		a.dirtyWorkspaces = make(map[string]bool)
	}
	a.dirtyWorkspaces[wsID] = true
	a.persistToken++
	token := a.persistToken
	return common.SafeTick(persistDebounce, func(t time.Time) tea.Msg {
		return persistDebounceMsg{token: token}
	})
}

func (a *App) migrateDirtyWorkspaceID(oldID, newID string) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if a.dirtyWorkspaces == nil || !a.dirtyWorkspaces[oldID] {
		return
	}
	a.dirtyWorkspaces[newID] = true
	delete(a.dirtyWorkspaces, oldID)
}

// persistActiveWorkspaceTabs is a convenience that persists the active workspace's tabs.
func (a *App) persistActiveWorkspaceTabs() tea.Cmd {
	if a.activeWorkspace == nil {
		return nil
	}
	return a.persistWorkspaceTabs(string(a.activeWorkspace.ID()))
}

func (a *App) handlePersistDebounce(msg persistDebounceMsg) tea.Cmd {
	// Ignore stale tokens (newer persist request superseded this one)
	if msg.token != a.persistToken {
		return nil
	}
	if a.center == nil || a.workspaceService == nil {
		return nil
	}
	if len(a.dirtyWorkspaces) == 0 {
		return nil
	}

	// Collect snapshots for all dirty workspaces
	var snapshots []*data.Workspace
	processed := make(map[string]bool, len(a.dirtyWorkspaces))
	for wsID := range a.dirtyWorkspaces {
		if a.isWorkspaceDeleteInFlight(wsID) {
			// Keep dirty marker while delete is in flight. If delete fails, the
			// marker must remain so pending workspace state can still be saved.
			continue
		}
		ws := a.findWorkspaceByID(wsID)
		if ws == nil {
			processed[wsID] = true
			continue
		}
		// Update in-memory state from center tabs
		tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
		ws.OpenTabs = tabs
		ws.ActiveTabIndex = activeIdx
		snapshots = append(snapshots, snapshotWorkspaceForSave(ws))
		processed[wsID] = true
	}
	// Clear only workspaces processed above; keep in-flight delete markers dirty.
	for wsID := range processed {
		delete(a.dirtyWorkspaces, wsID)
	}

	if len(snapshots) == 0 {
		return nil
	}
	service := a.workspaceService
	return func() tea.Msg {
		for _, snap := range snapshots {
			wsID := string(snap.ID())
			var saveErr error
			saved := a.runUnlessWorkspaceDeleteInFlight(wsID, func() {
				saveErr = service.Save(snap)
			})
			if !saved {
				continue
			}
			if saveErr != nil {
				logging.Warn("Failed to save workspace tabs: %v", saveErr)
			} else {
				// Marker bookkeeping is intentionally outside delete-state guard.
				// Delete safety is enforced by the guarded Save above.
				a.markLocalWorkspaceSaveForID(wsID)
			}
		}
		return nil
	}
}
