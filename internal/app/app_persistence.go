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
// This intentionally skips delete-in-flight workspaces. Saving during a
// destructive delete can recreate metadata after the delete removes it.
func (a *App) persistAllWorkspacesNow() {
	if a.workspaceService == nil || a.center == nil {
		return
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			wsID := string(ws.ID())
			if a.isWorkspaceDeleteInFlight(wsID) {
				continue
			}
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
	for k := range a.lifecycle.dirty {
		delete(a.lifecycle.dirty, k)
	}
}

// persistDebounceMsg is sent after the debounce period to trigger actual save.
type persistDebounceMsg struct {
	token persistToken
}

// persistSaveFailedMsg is returned by the debounced-save Cmd goroutine when one
// or more workspace saves fail. a.lifecycle.dirty is App state and must be
// mutated only on the Update loop (see workspaceLifecycleState.dirty's doc
// comment: "Touched only from App.Update handlers (single writer)"), so the
// goroutine cannot re-mark a workspace dirty itself. It reports the failure
// via this message instead; handlePersistSaveFailed does the actual re-dirty
// on the Update loop.
type persistSaveFailedMsg struct {
	workspaceIDs []string
}

// persistWorkspaceTabs marks a workspace dirty and schedules a debounced save.
func (a *App) persistWorkspaceTabs(wsID string) tea.Cmd {
	if wsID == "" {
		return nil
	}
	if a.isWorkspaceDeleteInFlight(wsID) {
		return nil
	}
	a.lifecycle.markDirty(wsID)
	a.lifecycle.persistToken++
	token := a.lifecycle.persistToken
	return common.SafeTick(persistDebounce, func(t time.Time) tea.Msg {
		return persistDebounceMsg{token: token}
	})
}

func (a *App) migrateDirtyWorkspaceID(oldID, newID string) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if a.lifecycle.dirty == nil || !a.lifecycle.dirty[oldID] {
		return
	}
	a.lifecycle.dirty[newID] = true
	delete(a.lifecycle.dirty, oldID)
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
	if msg.token != a.lifecycle.persistToken {
		return nil
	}
	if a.center == nil || a.workspaceService == nil {
		return nil
	}
	if len(a.lifecycle.dirty) == 0 {
		return nil
	}

	// Collect snapshots for all dirty workspaces
	var snapshots []*data.Workspace
	processed := make(map[string]bool, len(a.lifecycle.dirty))
	for wsID := range a.lifecycle.dirty {
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
		delete(a.lifecycle.dirty, wsID)
	}

	if len(snapshots) == 0 {
		return nil
	}
	service := a.workspaceService
	return func() tea.Msg {
		var failedIDs []string
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
				// Do not touch a.lifecycle.dirty here — this runs in a Cmd
				// goroutine, not on the Update loop. Report the failure via a
				// message so handlePersistSaveFailed can re-dirty safely.
				failedIDs = append(failedIDs, wsID)
			} else {
				// Marker bookkeeping is intentionally outside delete-state guard.
				// Delete safety is enforced by the guarded Save above.
				a.markLocalWorkspaceSaveForID(wsID)
			}
		}
		if len(failedIDs) == 0 {
			return nil
		}
		return persistSaveFailedMsg{workspaceIDs: failedIDs}
	}
}

// handlePersistSaveFailed re-marks workspaces dirty after their debounced save
// failed, so the next debounce (or clean shutdown) retries the save. This is
// the Update-loop counterpart to the persistSaveFailedMsg emitted from the
// save goroutine above: persistWorkspaceTabs mutates a.lifecycle.dirty, and it
// must only ever be called from here, not from that goroutine.
func (a *App) handlePersistSaveFailed(msg persistSaveFailedMsg) tea.Cmd {
	var cmds []tea.Cmd
	for _, wsID := range msg.workspaceIDs {
		if cmd := a.persistWorkspaceTabs(wsID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return common.SafeBatch(cmds...)
}
