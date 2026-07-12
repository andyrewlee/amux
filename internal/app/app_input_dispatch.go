package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// The updateXMsg sub-dispatchers below carve App.update's message switch
// into themed groups so the central routing function stays under the complexity
// ratchet. Each returns true (and appends to cmds) when it handled the message;
// the message types are disjoint across dispatchers, so call order is irrelevant.
//
// Routing map — which dispatcher owns a message type, and where its handlers
// live (see MESSAGE_FLOW.md for the create/activate and delete sequences):
//
//	handlePreSwitchInput   dialog/file-picker/settings/toast/overlay input
//	                       → app_input_dialogs.go
//	updateUpgradeMsg       UpdateCheckComplete, TriggerUpgrade, UpgradeComplete
//	                       → service_update.go
//	updateTabMsg           OpenDiff, CloseTab, LaunchAgent, TabCreated/Closed/
//	                       Detached/Reattached/StateChanged/SelectionChanged,
//	                       persistDebounceMsg, center.TabInputFailed
//	                       → app_input_messages_center.go, app_persistence.go
//	updateTmuxMsg          CleanupTmuxSessions, SpinnerTick, GitStatusTick,
//	                       OrphanGCTick, PTYWatchdogTick, tmuxActivityTick/
//	                       Result, tmuxAvailableResult, TmuxSyncTick,
//	                       tmuxTabsSyncResult, tmuxTabs/SidebarDiscoverResult,
//	                       orphanGCResult, staleDetachedAgentGCResult
//	                       → app_tmux*.go
//	updateWorkspaceLifecycleMsg  ProjectsLoaded, WorkspaceActivated/Created/
//	                       CreatedWithWarning/CreateFailed/SetupComplete,
//	                       CreateWorkspace, DeleteWorkspace, WorkspaceDeleted/
//	                       DeleteFailed, AddProject/RemoveProject/ProjectRemoved,
//	                       RefreshDashboard, RescanWorkspaces, GitStatusResult,
//	                       FileWatcherEvent, StateWatcherEvent
//	                       → app_input_messages_workspace.go, app_input_workspace.go
//	updateDialogShowMsg    Show* dialog requests, ThemePreview, SettingsResult
//	                       → app_input_dialogs.go

// handlePreSwitchInput runs the overlay/dialog guards that may consume a message
// before the main routing switch. It returns the resulting command and true when
// the message was consumed (the caller returns immediately).
func (a *App) handlePreSwitchInput(msg tea.Msg, cmds *[]tea.Cmd) (tea.Cmd, bool) {
	if perf.Enabled() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			a.markInput()
		}
	}

	if handled, cmd := a.handleDialogResultMsg(msg); handled {
		return cmd, true
	}
	if a.handleErrorOverlayDismiss(msg) {
		return nil, true
	}

	// Handle toast updates (does not consume the message).
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		*cmds = append(*cmds, cmd)
	}

	if a.handleDialogInput(msg, cmds) {
		return common.SafeBatch(*cmds...), true
	}
	if a.handleFilePickerInput(msg, cmds) {
		return common.SafeBatch(*cmds...), true
	}
	if a.handleSettingsDialogInput(msg, cmds) {
		return common.SafeBatch(*cmds...), true
	}
	return nil, false
}

// updateUpgradeMsg handles self-update / upgrade lifecycle messages.
func (a *App) updateUpgradeMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.UpdateCheckComplete:
		if cmd := a.handleUpdateCheckComplete(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TriggerUpgrade:
		if cmd := a.handleTriggerUpgrade(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.UpgradeComplete:
		if cmd := a.handleUpgradeComplete(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	default:
		return false
	}
	return true
}

// updateTabMsg handles center tab lifecycle and tab-state persistence messages.
func (a *App) updateTabMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.OpenDiff:
		if cmd := a.handleOpenDiff(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.CloseTab:
		*cmds = append(*cmds, a.center.CloseActiveTab())
	case messages.LaunchAgent:
		if cmd := a.handleLaunchAgent(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabCreated:
		if cmd := a.handleTabCreated(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		*cmds = append(*cmds, a.enforceAttachedAgentTabLimit()...)
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
		// Eagerly rescan so a just-started agent's working indicator does not
		// wait up to one ticker interval (~5s) to appear.
		if cmd := a.eagerScanTmuxActivity(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabClosed:
		logging.Info("Tab closed: %d", msg.Index)
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabDetached:
		logging.Info("Tab detached: %d", msg.Index)
		if cmd := a.handleTabDetached(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabReattached:
		*cmds = append(*cmds, a.enforceAttachedAgentTabLimit()...)
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
		// Eagerly rescan so a reattached agent's working indicator does not
		// wait up to one ticker interval (~5s) to appear.
		if cmd := a.eagerScanTmuxActivity(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.TabStateChanged:
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
	case messages.TabSelectionChanged:
		*cmds = append(*cmds, a.persistWorkspaceTabs(msg.WorkspaceID))
	case persistDebounceMsg:
		*cmds = append(*cmds, a.handlePersistDebounce(msg))
	case center.TabInputFailed:
		*cmds = append(*cmds, a.handleTabInputFailed(msg)...)
	default:
		return false
	}
	return true
}

// updateTmuxMsg handles tmux activity/sync, orphan-GC, and background-tick
// messages.
func (a *App) updateTmuxMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.CleanupTmuxSessions:
		if cmd := a.cleanupAllTmuxSessions(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case dashboard.SpinnerTickMsg:
		*cmds = append(*cmds, a.handleSpinnerTick(msg)...)
	case messages.GitStatusTick:
		*cmds = append(*cmds, a.handleGitStatusTick()...)
	case messages.OrphanGCTick:
		*cmds = append(*cmds, a.handleOrphanGCTick()...)
	case messages.PTYWatchdogTick:
		*cmds = append(*cmds, a.handlePTYWatchdogTick()...)
	case tmuxActivityTick:
		*cmds = append(*cmds, a.handleTmuxActivityTick(msg)...)
	case tmuxActivityResult:
		*cmds = append(*cmds, a.handleTmuxActivityResult(msg)...)
	case tmuxAvailableResult:
		*cmds = append(*cmds, a.handleTmuxAvailableResult(msg)...)
	case messages.TmuxSyncTick:
		*cmds = append(*cmds, a.handleTmuxSyncTick(msg)...)
	case tmuxTabsSyncResult:
		*cmds = append(*cmds, a.handleTmuxTabsSyncResult(msg)...)
	case tmuxTabsDiscoverResult:
		*cmds = append(*cmds, a.handleTmuxTabsDiscoverResult(msg)...)
	case tmuxSidebarDiscoverResult:
		*cmds = append(*cmds, a.handleTmuxSidebarDiscoverResult(msg)...)
	case orphanGCResult:
		a.handleOrphanGCResult(msg)
	case staleDetachedAgentGCResult:
		a.handleStaleDetachedAgentGCResult(msg)
	case sessionCountResult:
		a.handleSessionCountResult(msg)
	default:
		return false
	}
	return true
}

// updateWorkspaceLifecycleMsg handles project/workspace load, create, delete, and
// watcher messages.
func (a *App) updateWorkspaceLifecycleMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.ProjectsLoaded:
		*cmds = append(*cmds, a.handleProjectsLoaded(msg)...)
	case messages.WorkspaceActivated:
		*cmds = append(*cmds, a.handleWorkspaceActivated(msg)...)
	case messages.RefreshDashboard:
		*cmds = append(*cmds, a.loadProjects())
	case messages.RescanWorkspaces:
		*cmds = append(*cmds, a.rescanWorkspaces())
	case messages.WorkspaceCreatedWithWarning:
		*cmds = append(*cmds, a.handleWorkspaceCreatedWithWarning(msg)...)
	case messages.WorkspaceCreated:
		*cmds = append(*cmds, a.handleWorkspaceCreated(msg)...)
	case messages.WorkspaceSetupComplete:
		if cmd := a.handleWorkspaceSetupComplete(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.WorkspaceCreateFailed:
		if cmd := a.handleWorkspaceCreateFailed(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.GitStatusResult:
		if cmd := a.handleGitStatusResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.CreateWorkspace:
		*cmds = append(*cmds, a.handleCreateWorkspace(msg)...)
	case messages.DeleteWorkspace:
		*cmds = append(*cmds, a.handleDeleteWorkspace(msg)...)
	case messages.RenameWorkspace:
		*cmds = append(*cmds, a.handleRenameWorkspace(msg)...)
	case messages.AddProject:
		*cmds = append(*cmds, a.addProject(msg.Path))
	case messages.RemoveProject:
		*cmds = append(*cmds, a.removeProject(msg.Project))
	case messages.WorkspaceDeleted:
		*cmds = append(*cmds, a.handleWorkspaceDeleted(msg)...)
	case messages.ProjectRemoved:
		*cmds = append(*cmds, a.toast.ShowSuccess("Project removed"))
		*cmds = append(*cmds, a.loadProjects())
	case messages.WorkspaceDeleteFailed:
		if cmd := a.handleWorkspaceDeleteFailed(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.WorkspaceCommitted:
		if cmd := a.handleWorkspaceCommitted(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.FileWatcherEvent:
		*cmds = append(*cmds, a.handleFileWatcherEvent(msg)...)
	case messages.StateWatcherEvent:
		*cmds = append(*cmds, a.handleStateWatcherEvent(msg)...)
	default:
		return false
	}
	return true
}

// updateDialogShowMsg handles dialog/palette/settings show messages.
func (a *App) updateDialogShowMsg(msg tea.Msg, cmds *[]tea.Cmd) bool {
	switch msg := msg.(type) {
	case messages.ShowWelcome:
		a.goHome()
	case messages.ShowCommandsPalette:
		if cmd := a.openCommandsPalette(); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case messages.ToggleKeymapHints:
		a.setKeymapHintsEnabled(!a.config.UI.ShowKeymapHints)
		if err := a.config.SaveUISettings(); err != nil {
			*cmds = append(*cmds, common.ReportError("saving keymap setting", err, "Failed to save keymap setting"))
		}
	case messages.ShowQuitDialog:
		a.showQuitDialog()
	case messages.ShowAddProjectDialog:
		a.handleShowAddProjectDialog()
	case messages.ShowCreateWorkspaceDialog:
		a.handleShowCreateWorkspaceDialog(msg)
	case messages.ShowDeleteWorkspaceDialog:
		a.handleShowDeleteWorkspaceDialog(msg)
	case messages.ShowRenameWorkspaceDialog:
		a.handleShowRenameWorkspaceDialog(msg)
	case messages.ShowCommitWorkspaceDialog:
		a.handleShowCommitWorkspaceDialog(msg)
	case messages.ShowTrustScriptsDialog:
		a.handleShowTrustScriptsDialog(msg)
	case messages.ShowRemoveProjectDialog:
		a.handleShowRemoveProjectDialog(msg)
	case messages.ShowSelectAssistantDialog:
		a.handleShowSelectAssistantDialog()
	case messages.ShowSettingsDialog:
		a.handleShowSettingsDialog()
	case messages.ShowCleanupTmuxDialog:
		a.handleShowCleanupTmuxDialog()
	case common.ThemePreview:
		if cmd := a.handleThemePreview(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	case common.SettingsResult:
		if cmd := a.handleSettingsResult(msg); cmd != nil {
			*cmds = append(*cmds, cmd)
		}
	default:
		return false
	}
	return true
}
