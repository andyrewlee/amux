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

	// Handle dialog result first (arrives after dialog is hidden)
	if result, ok := msg.(common.DialogResult); ok {
		logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
		switch result.ID {
		case DialogAddProject, DialogCreateWorkspace, DialogDeleteWorkspace, DialogRemoveProject, DialogSelectAssistant, "agent-picker", DialogQuit, DialogCleanupTmux:
			return a, a.safeCmd(a.handleDialogResult(result))
		}
		// If not an App-level dialog, let it fall through to components
		// Currently only Center uses custom dialogs
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return a, a.safeCmd(cmd)
	}

	// Handle help overlay input (highest priority when visible)
	if a.helpOverlay.Visible() {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			var cmd tea.Cmd
			a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
			return a, a.safeCmd(cmd)
		case tea.MouseWheelMsg:
			a.helpOverlay, _, _ = a.helpOverlay.Update(msg)
			return a, nil
		case tea.MouseClickMsg:
			if msg.Button == tea.MouseLeft {
				// First check if clicking on a link inside the dialog
				var cmd tea.Cmd
				a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
				if cmd != nil {
					return a, a.safeCmd(cmd)
				}
				// Close if clicking outside the dialog
				if !a.helpOverlay.ContainsClick(msg.X, msg.Y) {
					a.helpOverlay.Hide()
				}
				return a, nil
			}
		}
	}

	// Allow clicking to dismiss error overlays
	if mouseMsg, ok := msg.(tea.MouseClickMsg); ok && mouseMsg.Button == tea.MouseLeft {
		if a.err != nil {
			a.err = nil
			return a, nil
		}
	}

	// Handle toast updates
	if _, ok := msg.(common.ToastDismissed); ok {
		newToast, cmd := a.toast.Update(msg)
		a.toast = newToast
		cmds = append(cmds, cmd)
	}

	// Handle dialog input if visible
	if a.dialog != nil && a.dialog.Visible() {
		newDialog, cmd := a.dialog.Update(msg)
		a.dialog = newDialog
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while dialog is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	// Handle file picker if visible
	if a.filePicker != nil && a.filePicker.Visible() {
		newPicker, cmd := a.filePicker.Update(msg)
		a.filePicker = newPicker
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while file picker is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	// Handle settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		newSettings, cmd := a.settingsDialog.Update(msg)
		a.settingsDialog = newSettings
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while settings dialog is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		a.keyboardEnhancements = msg
		logging.Info("Keyboard enhancements: disambiguation=%t event_types=%t", msg.SupportsKeyDisambiguation(), msg.SupportsEventTypes())

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.layout.Resize(msg.Width, msg.Height)
		a.updateLayout()
		// Update help overlay size for accurate hit-testing after resize
		if a.helpOverlay.Visible() {
			a.helpOverlay.SetSize(a.width, a.height)
		}

	case tea.MouseClickMsg:
		if cmd := a.routeMouseClick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseWheelMsg:
		if cmd := a.routeMouseWheel(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMotionMsg:
		if cmd := a.routeMouseMotion(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseReleaseMsg:
		if cmd := a.routeMouseRelease(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		switch a.focusedPane {
		case messages.PaneCenter:
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneSidebarTerminal:
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case prefixTimeoutMsg:
		if msg.token == a.prefixToken && a.prefixActive {
			a.exitPrefix()
		}

	case tea.KeyPressMsg:
		if cmd := a.handleKeyPress(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ProjectsLoaded:
		cmds = append(cmds, a.handleProjectsLoaded(msg)...)

	case messages.WorkspaceActivated:
		cmds = append(cmds, a.handleWorkspaceActivated(msg)...)

	case messages.WorkspacePreviewed:
		cmds = append(cmds, a.handleWorkspacePreviewed(msg)...)

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
		cmds = append(cmds, a.persistActiveWorkspaceTabs())

	case messages.TabReattached:
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
		a.err = msg.Err
		logging.Error("Error in %s: %v", msg.Context, msg.Err)

	default:
		// Forward unknown messages to center pane (e.g., commit viewer internal messages)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, a.safeBatch(cmds...)
}
