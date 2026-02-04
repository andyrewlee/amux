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
func (a *App) persistAllWorkspacesNow() {
	if a.workspaceService == nil || a.center == nil {
		return
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			wsID := string(ws.ID())
			tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
			if len(tabs) == 0 {
				continue
			}
			ws.OpenTabs = tabs
			ws.ActiveTabIndex = activeIdx
			snap := snapshotWorkspaceForSave(ws)
			if err := a.workspaceService.Save(snap); err != nil {
				logging.Warn("Failed to persist workspace on shutdown: %v", err)
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
	a.dirtyWorkspaces[wsID] = true
	a.persistToken++
	token := a.persistToken
	return common.SafeTick(persistDebounce, func(t time.Time) tea.Msg {
		return persistDebounceMsg{token: token}
	})
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
	if len(a.dirtyWorkspaces) == 0 {
		return nil
	}

	// Collect snapshots for all dirty workspaces
	var snapshots []*data.Workspace
	for wsID := range a.dirtyWorkspaces {
		ws := a.findWorkspaceByID(wsID)
		if ws == nil {
			continue
		}
		// Update in-memory state from center tabs
		tabs, activeIdx := a.center.GetTabsInfoForWorkspace(wsID)
		ws.OpenTabs = tabs
		ws.ActiveTabIndex = activeIdx
		snapshots = append(snapshots, snapshotWorkspaceForSave(ws))
	}
	// Clear dirty set
	for k := range a.dirtyWorkspaces {
		delete(a.dirtyWorkspaces, k)
	}

	if len(snapshots) == 0 {
		return nil
	}
	service := a.workspaceService
	return func() tea.Msg {
		for _, snap := range snapshots {
			if err := service.Save(snap); err != nil {
				logging.Warn("Failed to save workspace tabs: %v", err)
			}
		}
		return nil
	}
}
