package app

import (
	"fmt"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// Update handles all messages with panic recovery.
func (a *App) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in app.Update: %v\n%s", r, debug.Stack())
			a.err = fmt.Errorf("internal error: %v", r)
			model = a
			cmd = nil
		}
	}()
	return a.update(msg)
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer perf.Time("update")()
	var cmds []tea.Cmd
	if perf.Enabled() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.KeyReleaseMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			a.markInput()
		}
	}

	if handled, cmd := a.handleDialogResultMsg(msg); handled {
		return a, cmd
	}
	if handled, cmd := a.handleHelpOverlayInput(msg); handled {
		return a, cmd
	}
	if a.handleErrorOverlayDismiss(msg) {
		return a, nil
	}

	// Handle toast updates
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		cmds = append(cmds, cmd)
	}

	if a.handleDialogInput(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.handleFilePickerInput(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.handleSettingsDialogInput(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		a.handleKeyboardEnhancements(msg)

	case tea.WindowSizeMsg:
		a.handleWindowSize(msg)

	case tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if cmd := a.handleMouseMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		if cmd := a.handlePaste(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case prefixTimeoutMsg:
		a.handlePrefixTimeout(msg)

	case tea.KeyPressMsg:
		if cmd := a.handleKeyPress(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ProjectsLoaded:
		cmds = append(cmds, a.handleProjectsLoaded(msg)...)

	case messages.WorkspaceActivated:
		cmds = append(cmds, a.handleWorkspaceActivated(msg)...)

	case messages.ShowWelcome:
		a.goHome()

	case messages.ToggleHelp:
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()

	case messages.ToggleKeymapHints:
		a.setKeymapHintsEnabled(!a.config.UI.ShowKeymapHints)
		if err := a.config.SaveUISettings(); err != nil {
			cmds = append(cmds, a.toast.ShowWarning("Failed to save keymap setting"))
		}

	case messages.ShowQuitDialog:
		a.showQuitDialog()

	case messages.RefreshDashboard:
		cmds = append(cmds, a.loadProjects())

	case messages.RescanWorkspaces:
		cmds = append(cmds, a.rescanWorkspaces())

	case messages.WorkspaceCreatedWithWarning:
		cmds = append(cmds, a.handleWorkspaceCreatedWithWarning(msg)...)

	case messages.WorkspaceCreated:
		cmds = append(cmds, a.handleWorkspaceCreated(msg)...)

	case messages.WorkspaceSetupComplete:
		if cmd := a.handleWorkspaceSetupComplete(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.WorkspaceCreateFailed:
		if cmd := a.handleWorkspaceCreateFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GitStatusResult:
		if cmd := a.handleGitStatusResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowAddProjectDialog:
		a.handleShowAddProjectDialog()

	case messages.ShowCreateWorkspaceDialog:
		a.handleShowCreateWorkspaceDialog(msg)

	case messages.ShowDeleteWorkspaceDialog:
		a.handleShowDeleteWorkspaceDialog(msg)

	case messages.ShowRemoveProjectDialog:
		a.handleShowRemoveProjectDialog(msg)

	case messages.ShowSelectAssistantDialog:
		a.handleShowSelectAssistantDialog()

	case messages.ShowSettingsDialog:
		a.handleShowSettingsDialog()

	case messages.ShowCleanupTmuxDialog:
		a.handleShowCleanupTmuxDialog()

	case common.ThemePreview:
		a.handleThemePreview(msg)

	case common.SettingsResult:
		if cmd := a.handleSettingsResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CreateWorkspace:
		cmds = append(cmds, a.handleCreateWorkspace(msg)...)

	case messages.DeleteWorkspace:
		cmds = append(cmds, a.handleDeleteWorkspace(msg)...)

	case messages.CleanupTmuxSessions:
		if cmd := a.cleanupAllTmuxSessions(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.AddProject:
		cmds = append(cmds, a.addProject(msg.Path))

	case messages.RemoveProject:
		cmds = append(cmds, a.removeProject(msg.Project))

	case messages.OpenDiff:
		if cmd := a.handleOpenDiff(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CloseTab:
		cmd := a.center.CloseActiveTab()
		cmds = append(cmds, cmd)

	case messages.LaunchAgent:
		if cmd := a.handleLaunchAgent(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.TabCreated:
		if cmd := a.handleTabCreated(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, a.enforceAttachedAgentTabLimit()...)
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.TabClosed:
		logging.Info("Tab closed: %d", msg.Index)
		if cmd := a.persistActiveWorkspaceTabs(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.TabDetached:
		logging.Info("Tab detached: %d", msg.Index)
		if cmd := a.handleTabDetached(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.TabReattached:
		cmds = append(cmds, a.enforceAttachedAgentTabLimit()...)
		cmds = append(cmds, a.persistWorkspaceTabs(msg.WorkspaceID))

	case messages.TabStateChanged:
		cmds = append(cmds, a.persistWorkspaceTabs(msg.WorkspaceID))

	case messages.TabSelectionChanged:
		cmds = append(cmds, a.persistWorkspaceTabs(msg.WorkspaceID))

	case persistDebounceMsg:
		cmds = append(cmds, a.handlePersistDebounce(msg))

	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		if cmd := a.handlePTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Sync active agents state to dashboard (show spinner only when actively outputting)
		a.syncActiveWorkspacesToDashboard()
		if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
			cmds = append(cmds, startCmd)
		}

	case center.TabInputFailed:
		cmds = append(cmds, a.handleTabInputFailed(msg)...)

	case messages.Toast:
		switch msg.Level {
		case messages.ToastSuccess:
			cmds = append(cmds, a.toast.ShowSuccess(msg.Message))
		case messages.ToastError:
			cmds = append(cmds, a.toast.ShowError(msg.Message))
		case messages.ToastWarning:
			cmds = append(cmds, a.toast.ShowWarning(msg.Message))
		default:
			cmds = append(cmds, a.toast.ShowInfo(msg.Message))
		}

	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, messages.SidebarPTYRestart, sidebar.SidebarTerminalCreated, sidebar.SidebarTerminalCreateFailed, sidebar.SidebarTerminalReattachResult, sidebar.SidebarTerminalReattachFailed, sidebar.SidebarSelectionScrollTick:
		if cmd := a.handleSidebarPTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case sidebar.OpenFileInEditor:
		if cmd := a.handleOpenFileInEditor(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case dashboard.SpinnerTickMsg:
		cmds = append(cmds, a.handleSpinnerTick(msg)...)

	case messages.GitStatusTick:
		cmds = append(cmds, a.handleGitStatusTick()...)

	case messages.PTYWatchdogTick:
		cmds = append(cmds, a.handlePTYWatchdogTick()...)
	case tmuxActivityTick:
		cmds = append(cmds, a.handleTmuxActivityTick(msg)...)
	case tmuxActivityResult:
		cmds = append(cmds, a.handleTmuxActivityResult(msg)...)
	case tmuxAvailableResult:
		cmds = append(cmds, a.handleTmuxAvailableResult(msg)...)
	case messages.TmuxSyncTick:
		cmds = append(cmds, a.handleTmuxSyncTick(msg)...)

	case tmuxTabsSyncResult:
		cmds = append(cmds, a.handleTmuxTabsSyncResult(msg)...)
	case tmuxTabsDiscoverResult:
		cmds = append(cmds, a.handleTmuxTabsDiscoverResult(msg)...)
	case tmuxSidebarDiscoverResult:
		cmds = append(cmds, a.handleTmuxSidebarDiscoverResult(msg)...)
	case orphanGCResult:
		a.handleOrphanGCResult(msg)
	case terminalGCResult:
		a.handleTerminalGCResult(msg)

	case messages.FileWatcherEvent:
		cmds = append(cmds, a.handleFileWatcherEvent(msg)...)

	case messages.StateWatcherEvent:
		cmds = append(cmds, a.handleStateWatcherEvent(msg)...)

	case messages.WorkspaceDeleted:
		cmds = append(cmds, a.handleWorkspaceDeleted(msg)...)

	case messages.ProjectRemoved:
		cmds = append(cmds, a.toast.ShowSuccess("Project removed"))
		cmds = append(cmds, a.loadProjects())

	case messages.WorkspaceDeleteFailed:
		if cmd := a.handleWorkspaceDeleteFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.UpdateCheckComplete:
		if cmd := a.handleUpdateCheckComplete(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.TriggerUpgrade:
		if cmd := a.handleTriggerUpgrade(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.UpgradeComplete:
		if cmd := a.handleUpgradeComplete(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.Error:
		if cmd := a.handleErrorMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Forward unknown messages to center pane (e.g., commit viewer internal messages)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, common.SafeBatch(cmds...)
}

func (a *App) handleTabDetached(msg messages.TabDetached) tea.Cmd {
	if msg.WorkspaceID != "" {
		return a.persistWorkspaceTabs(msg.WorkspaceID)
	}
	return a.persistActiveWorkspaceTabs()
}
