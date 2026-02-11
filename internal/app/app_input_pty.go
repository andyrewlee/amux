package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/center"
	"github.com/andyrewlee/medusa/internal/ui/dashboard"
)

// handlePTYMessages handles PTY-related messages for center pane.
func (a *App) handlePTYMessages(msg tea.Msg) tea.Cmd {
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleSidebarPTYMessages handles PTY-related messages for sidebar terminal.
func (a *App) handleSidebarPTYMessages(msg tea.Msg) tea.Cmd {
	newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
	a.sidebarTerminal = newSidebarTerminal
	return cmd
}

// handleGitStatusTick handles the GitStatusTick message.
func (a *App) handleGitStatusTick() []tea.Cmd {
	var cmds []tea.Cmd
	// Only refresh git status periodically when sidebar is visible
	if a.activeWorkspace != nil && !a.layout.SidebarHidden() {
		cmds = append(cmds, a.requestGitStatusCached(a.activeWorkspace.Root))
	}
	// Refresh active workspace indicators even when no PTY output is flowing.
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	cmds = append(cmds, a.startGitStatusTicker())
	return cmds
}

// handleFileWatcherEvent handles the FileWatcherEvent message.
func (a *App) handleFileWatcherEvent(msg messages.FileWatcherEvent) []tea.Cmd {
	// Always re-listen for the next event
	cmds := []tea.Cmd{a.startFileWatcher()}
	if !a.layout.SidebarHidden() {
		a.statusManager.Invalidate(msg.Root)
		a.dashboard.InvalidateStatus(msg.Root)
		cmds = append(cmds, a.requestGitStatus(msg.Root))
	}
	return cmds
}

// handleTabInputFailed handles the TabInputFailed message.
func (a *App) handleTabInputFailed(msg center.TabInputFailed) []tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowWarning("Session disconnected - scroll history preserved"))
	if msg.WorkspaceID != "" {
		if cmd := a.center.DetachTabByID(msg.WorkspaceID, msg.TabID); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Auto-reattach: immediately try to reconnect the detached tab
		if cmd := a.center.ReattachTabByID(msg.WorkspaceID, msg.TabID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// handleSpinnerTick handles the SpinnerTickMsg from dashboard.
func (a *App) handleSpinnerTick(msg dashboard.SpinnerTickMsg) []tea.Cmd {
	var cmds []tea.Cmd
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	a.center.TickSpinner()
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// handlePTYWatchdogTick handles the PTYWatchdogTick message.
func (a *App) handlePTYWatchdogTick() []tea.Cmd {
	var cmds []tea.Cmd
	if a.center != nil {
		if cmd := a.center.StartPTYReaders(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if a.sidebarTerminal != nil && !a.layout.SidebarHidden() {
		if cmd := a.sidebarTerminal.StartPTYReaders(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Keep dashboard "working" state accurate even when agents go idle.
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	cmds = append(cmds, a.startPTYWatchdog())
	return cmds
}
