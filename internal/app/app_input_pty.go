package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
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
	if a.activeWorkspace != nil {
		cmds = append(cmds, a.requestGitStatusCached(a.activeWorkspace.Root, true))
	}
	// Refresh active workspace indicators even when no PTY output is flowing.
	a.syncActiveWorkspacesToDashboard()
	cmds = append(cmds, a.startGitStatusTicker())
	return cmds
}

// handleFileWatcherEvent handles the FileWatcherEvent message.
func (a *App) handleFileWatcherEvent(msg messages.FileWatcherEvent) []tea.Cmd {
	requestRoot := msg.Root
	requestFull := false
	if a.gitStatus != nil {
		a.gitStatus.Invalidate(msg.Root)
	}
	a.dashboard.InvalidateStatus(msg.Root)
	if a.activeWorkspace != nil && rootsReferToSameWorkspace(msg.Root, a.activeWorkspace.Root) {
		requestRoot = a.activeWorkspace.Root
		requestFull = true
		if a.gitStatus != nil {
			a.gitStatus.Invalidate(requestRoot)
		}
		a.dashboard.InvalidateStatus(requestRoot)
	}
	statusCmd := a.requestGitStatus(requestRoot)
	if requestFull {
		statusCmd = a.requestGitStatusFull(requestRoot)
	}
	return []tea.Cmd{
		statusCmd,
		a.startFileWatcher(),
	}
}

// handleStateWatcherEvent handles changes to amux state files (projects/workspaces).
func (a *App) handleStateWatcherEvent(msg messages.StateWatcherEvent) []tea.Cmd {
	if msg.Reason == "workspaces" && a.shouldSuppressWorkspaceReload(msg.Paths, time.Now()) {
		return []tea.Cmd{
			a.startStateWatcher(),
		}
	}
	return []tea.Cmd{
		a.loadProjects(),
		a.startStateWatcher(),
	}
}

// handleTabInputFailed handles the TabInputFailed message.
func (a *App) handleTabInputFailed(msg center.TabInputFailed) []tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowWarning("Session disconnected - scroll history preserved"))
	if msg.WorkspaceID != "" {
		if cmd := a.center.DetachTabByID(msg.WorkspaceID, msg.TabID); cmd != nil {
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
	a.syncActiveWorkspacesToDashboard()
	a.center.TickSpinner()
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
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
	if a.sidebarTerminal != nil {
		if cmd := a.sidebarTerminal.StartPTYReaders(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Keep dashboard "working" state accurate even when agents go idle.
	a.syncActiveWorkspacesToDashboard()
	cmds = append(cmds, a.startPTYWatchdog())
	return cmds
}
