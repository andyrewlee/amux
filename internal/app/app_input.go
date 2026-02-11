package app

import (
	"fmt"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/perf"
	"github.com/andyrewlee/medusa/internal/ui/center"
	"github.com/andyrewlee/medusa/internal/ui/common"
	"github.com/andyrewlee/medusa/internal/ui/dashboard"
	"github.com/andyrewlee/medusa/internal/ui/sidebar"
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
		case DialogAddProject, DialogCreateWorkspace, DialogDeleteWorkspace, DialogRemoveProject, DialogSelectAssistant, "agent-picker", DialogQuit, DialogCleanupTmux, DialogSetProfile, DialogRenameWorkspace, DialogRenameProfile, DialogCreateProfile, DialogDeleteProfile,
			DialogCreateGroup, DialogAddGroupRepo, DialogCreateGroupWorkspace, DialogDeleteGroup, DialogDeleteGroupWorkspace, DialogSetGroupProfile, DialogRenameGroupWorkspace, DialogRenameGroup, DialogCommit:
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

	// Handle theme dialog if visible
	if a.themeDialog != nil && a.themeDialog.Visible() {
		newTheme, cmd := a.themeDialog.Update(msg)
		a.themeDialog = newTheme
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Don't process other input while theme dialog is open
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	// Handle permissions dialog if visible
	if a.permissionsDialog != nil && a.permissionsDialog.Visible() {
		newDialog, cmd := a.permissionsDialog.Update(msg)
		a.permissionsDialog = newDialog
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	// Handle permissions editor if visible
	if a.permissionsEditor != nil && a.permissionsEditor.Visible() {
		newEditor, cmd := a.permissionsEditor.Update(msg)
		a.permissionsEditor = newEditor
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return a, a.safeBatch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, a.safeBatch(cmds...)
		}
	}

	// Handle profile manager if visible
	if a.profileManager != nil && a.profileManager.Visible() {
		newManager, cmd := a.profileManager.Update(msg)
		a.profileManager = newManager
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
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
		if a.monitorMode {
			if cmd := a.handleMonitorModeClick(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		if cmd := a.routeMouseClick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseWheelMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseWheel(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMotionMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseMotion(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseReleaseMsg:
		if a.monitorMode {
			break
		}
		if cmd := a.routeMouseRelease(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		// Handle paste in monitor mode - forward to selected tile
		if a.monitorMode && a.focusedPane == messages.PaneMonitor {
			tabs := a.filterMonitorTabs(a.center.MonitorTabs())
			if len(tabs) > 0 {
				idx := a.center.MonitorSelectedIndex(len(tabs))
				if cmd := a.center.HandleMonitorInput(tabs[idx].ID, msg); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			break
		}
		// Non-monitor paste handling falls through to focused pane
		switch a.focusedPane {
		case messages.PaneCenter:
			newCenter, cmd := a.center.Update(msg)
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case messages.PaneTerminal:
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

	case messages.ToggleMonitor:
		if cmd := a.toggleMonitorMode(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ToggleHelp:
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()

	case messages.ToggleKeymapHints:
		a.setKeymapHintsEnabled(!a.config.UI.ShowKeymapHints)
		if err := a.config.SaveUISettings(); err != nil {
			cmds = append(cmds, a.toast.ShowWarning("Failed to save keymap setting"))
		}

	case messages.ToggleTerminalCollapse:
		a.layout.ToggleTerminalCollapsed()
		a.updateLayout()

	case messages.ShowQuitDialog:
		a.showQuitDialog()

	case messages.RefreshDashboard:
		cmds = append(cmds, a.loadProjects())
		cmds = append(cmds, a.loadGroups())

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

	case messages.ShowRenameWorkspaceDialog:
		a.handleShowRenameWorkspaceDialog(msg)

	case messages.RenameWorkspace:
		if cmd := a.handleRenameWorkspace(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowDeleteWorkspaceDialog:
		a.handleShowDeleteWorkspaceDialog(msg)

	case messages.ShowRemoveProjectDialog:
		a.handleShowRemoveProjectDialog(msg)

	case messages.ShowSetProfileDialog:
		if a.projectHasActiveSessions(msg.Project) {
			cmds = append(cmds, a.toast.ShowError("Cannot change profile while workspaces have active sessions"))
			break
		}
		a.handleShowSetProfileDialog(msg)

	case messages.SetProfile:
		if cmd := a.handleSetProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowRenameProfileDialog:
		a.handleShowRenameProfileDialog(msg)

	case messages.RenameProfile:
		if cmd := a.handleRenameProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowCreateProfileDialog:
		a.handleShowCreateProfileDialog()

	case messages.CreateProfile:
		if cmd := a.handleCreateProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowDeleteProfileDialog:
		a.handleShowDeleteProfileDialog(msg)

	case messages.DeleteProfile:
		if cmd := a.handleDeleteProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowSelectAssistantDialog:
		if cmd := a.handleShowSelectAssistantDialog(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowSettingsDialog:
		a.handleShowSettingsDialog()

	case messages.ShowCleanupTmuxDialog:
		a.handleShowCleanupTmuxDialog()

	case common.ThemePreview:
		a.handleThemePreview(msg)

	case common.ShowThemeEditor:
		a.handleShowThemeEditor()

	case common.ThemeResult:
		if cmd := a.handleThemeResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

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
		a.pendingNewProjectPath = msg.Path
		cmds = append(cmds, a.addProject(msg.Path))

	case messages.RemoveProject:
		if msg.Project != nil {
			// Clean up tmux sessions, center tabs, and sidebar terminal
			// for all workspaces in the project before removing it.
			for i := range msg.Project.Workspaces {
				ws := &msg.Project.Workspaces[i]
				if cleanup := a.cleanupWorkspaceTmuxSessions(ws); cleanup != nil {
					cmds = append(cmds, cleanup)
				}
				newCenter, cmd := a.center.Update(messages.WorkspaceDeleted{Workspace: ws})
				a.center = newCenter
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				newTerminal, cmd := a.sidebarTerminal.Update(messages.WorkspaceDeleted{Workspace: ws})
				a.sidebarTerminal = newTerminal
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			// Unwatch permissions for all workspaces of this project
			if a.permissionWatcher != nil {
				for _, ws := range msg.Project.Workspaces {
					a.permissionWatcher.Unwatch(ws.Root)
				}
			}
		}
		cmds = append(cmds, a.removeProject(msg.Project))

	case messages.OpenDiff:
		if cmd := a.handleOpenDiff(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CloseTab:
		cmd := a.center.CloseActiveTab()
		cmds = append(cmds, cmd)

	case messages.LaunchAgent:
		if msg.Workspace != nil && msg.Workspace.Profile == "" {
			a.pendingProfileLaunch = msg.Assistant
			a.pendingProfileLaunchRoot = msg.Workspace.Root
			project := a.findProjectForWorkspace(msg.Workspace)
			if project != nil {
				a.handleShowSetProfileDialog(messages.ShowSetProfileDialog{Project: project})
			} else if a.activeGroupWs != nil {
				// Group workspace — show group profile dialog
				a.handleShowSetGroupProfileDialog(messages.ShowSetGroupProfileDialog{Group: a.activeGroup})
			}
			break
		}
		// For group workspaces, trust the group root (parent of all repo worktrees)
		if a.activeGroupWs != nil && msg.Workspace != nil {
			profileDir := ""
			if msg.Workspace.Profile != "" {
				profileDir = filepath.Join(a.config.Paths.ProfilesRoot, msg.Workspace.Profile)
			}
			_ = config.InjectTrustedDirectory(a.activeGroupWs.Primary.Root, profileDir)
			if a.activeGroupWs.AllowEdits {
				_ = config.InjectAllowEdits(a.activeGroupWs.Primary.Root)
			}
		}
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
		if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
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

	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, messages.SidebarPTYRestart, sidebar.SidebarTerminalCreated, sidebar.SidebarTerminalCreateFailed:
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

	case messages.PermissionWatcherEvent:
		cmds = append(cmds, a.handlePermissionWatcherEvent(msg)...)

	case messages.PermissionDetected:
		if cmd := a.handlePermissionDetected(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowPermissionsDialog:
		if len(a.pendingPermissions) > 0 {
			a.permissionsDialog = common.NewPermissionsDialog(a.pendingPermissions)
			a.permissionsDialog.SetSize(a.width, a.height)
			a.permissionsDialog.Show()
		}

	case common.ShowPermissionsEditor:
		global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
		if err != nil {
			cmds = append(cmds, a.toast.ShowError("Failed to load global permissions"))
		} else {
			a.permissionsEditor = common.NewPermissionsEditor(global.Allow, global.Deny)
			a.permissionsEditor.SetSize(a.width, a.height)
			a.permissionsEditor.Show()
		}

	case common.ShowProfileManager:
		profiles := a.listProfiles()
		a.profileManager = common.NewProfileManager(profiles)
		a.profileManager.SetSize(a.width, a.height)
		a.profileManager.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.profileManager.Show()

	case common.ProfileManagerResult:
		a.profileManager = nil
		// Re-show settings dialog after closing profile manager
		a.handleShowSettingsDialog()

	case messages.PermissionsDialogResult:
		if cmd := a.handlePermissionsDialogResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.PermissionsEditorResult:
		if cmd := a.handlePermissionsEditorResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.FileWatcherEvent:
		cmds = append(cmds, a.handleFileWatcherEvent(msg)...)

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

	case messages.ActionBarCopyDir:
		if cmd := a.handleActionBarCopyDir(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ActionBarOpenIDE:
		if cmd := a.handleActionBarOpenIDE(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ActionBarMergeToMain:
		cmds = append(cmds, a.handleActionBarMergeToMain(msg))

	case messages.ActionBarCommitResult:
		if cmd := a.handleActionBarCommitResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ActionBarMergeResult:
		if cmd := a.handleActionBarMergeResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ActionBarOpenMR:
		if cmd := a.handleActionBarOpenMR(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowCommitDialog:
		a.handleShowCommitDialog(msg)

	// --- Group messages ---
	case messages.GroupsLoaded:
		a.groups = msg.Groups
		a.dashboard.SetGroups(a.groups)
		// Eagerly restore agent tabs for all group workspaces on startup.
		for i := range a.groups {
			for j := range a.groups[i].Workspaces {
				ws := &a.groups[i].Workspaces[j].Primary
				if workspaceHasLiveTabs(ws) {
					if restoreCmd := a.center.RestoreTabsFromWorkspace(ws); restoreCmd != nil {
						cmds = append(cmds, restoreCmd)
					}
				}
			}
		}
		// Auto-activate a newly created group workspace for auto-launch.
		if a.pendingGroupAutoLaunch != "" {
			wsName := a.pendingGroupAutoLaunch
			for i := range a.groups {
				for j := range a.groups[i].Workspaces {
					gw := &a.groups[i].Workspaces[j]
					if gw.Name == wsName {
						a.pendingGroupAutoLaunch = ""
						group := &a.groups[i]
						cmds = append(cmds, func() tea.Msg {
							return messages.GroupWorkspaceActivated{
								Group:     group,
								Workspace: gw,
							}
						})
						goto groupPendingFound
					}
				}
			}
		groupPendingFound:
		}

	case messages.ShowCreateGroupDialog:
		a.handleShowCreateGroupDialog()

	case messages.CreateGroup:
		cmds = append(cmds, a.createGroup(msg.Name, msg.RepoPaths, msg.Profile))

	case messages.GroupCreated:
		cmds = append(cmds, a.toast.ShowSuccess("Group '"+msg.Name+"' created"))
		cmds = append(cmds, a.loadGroups())
		cmds = append(cmds, a.loadProjects())

	case messages.ShowDeleteGroupDialog:
		a.handleShowDeleteGroupDialog(msg)

	case messages.RemoveGroup:
		// Clean up tmux sessions, center tabs, sidebar terminal, and persisted
		// tab state for all workspaces in the group before removing it.
		for i := range a.groups {
			if a.groups[i].Name != msg.Name {
				continue
			}
			for j := range a.groups[i].Workspaces {
				gw := &a.groups[i].Workspaces[j]
				if cleanup := a.cleanupWorkspaceTmuxSessions(&gw.Primary); cleanup != nil {
					cmds = append(cmds, cleanup)
				}
				newCenter, cmd := a.center.Update(messages.WorkspaceDeleted{Workspace: &gw.Primary})
				a.center = newCenter
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				newTerminal, cmd := a.sidebarTerminal.Update(messages.WorkspaceDeleted{Workspace: &gw.Primary})
				a.sidebarTerminal = newTerminal
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				_ = a.workspaces.Delete(gw.Primary.ID())
			}
			break
		}
		cmds = append(cmds, a.removeGroup(msg.Name))

	case messages.GroupRemoved:
		cmds = append(cmds, a.toast.ShowSuccess("Group removed"))
		cmds = append(cmds, a.loadGroups())

	case messages.ShowCreateGroupWorkspaceDialog:
		a.handleShowCreateGroupWorkspaceDialog(msg)

	case messages.CreateGroupWorkspace:
		var group *data.ProjectGroup
		for i := range a.groups {
			if a.groups[i].Name == msg.GroupName {
				group = &a.groups[i]
				break
			}
		}
		if group != nil {
			cmds = append(cmds, a.createGroupWorkspace(group, msg.Name, msg.AllowEdits, msg.LoadClaudeMD))
		}

	case messages.GroupWorkspaceCreated:
		cmds = append(cmds, a.toast.ShowSuccess("Group workspace created"))
		if msg.Workspace != nil {
			a.pendingGroupAutoLaunch = msg.Workspace.Name
		}
		cmds = append(cmds, a.loadGroups())

	case messages.GroupWorkspaceCreateFailed:
		errMsg := "Failed to create group workspace"
		if msg.Err != nil {
			errMsg += ": " + msg.Err.Error()
		}
		cmds = append(cmds, a.toast.ShowError(errMsg))

	case messages.ShowRenameGroupDialog:
		a.handleShowRenameGroupDialog(msg)

	case messages.RenameGroup:
		if cmd := a.handleRenameGroup(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowRenameGroupWorkspaceDialog:
		a.handleShowRenameGroupWorkspaceDialog(msg)

	case messages.RenameGroupWorkspace:
		if cmd := a.handleRenameGroupWorkspace(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowDeleteGroupWorkspaceDialog:
		a.handleShowDeleteGroupWorkspaceDialog(msg)

	case messages.DeleteGroupWorkspace:
		if msg.Workspace != nil {
			if cleanup := a.cleanupWorkspaceTmuxSessions(&msg.Workspace.Primary); cleanup != nil {
				cmds = append(cmds, cleanup)
			}
		}
		cmds = append(cmds, a.deleteGroupWorkspace(msg.Group, msg.Workspace))

	case messages.GroupWorkspaceDeleted:
		if msg.Workspace != nil {
			if cleanup := a.cleanupWorkspaceTmuxSessions(&msg.Workspace.Primary); cleanup != nil {
				cmds = append(cmds, cleanup)
			}
			// Close center tabs and sidebar terminal for this workspace
			newCenter, cmd := a.center.Update(messages.WorkspaceDeleted{Workspace: &msg.Workspace.Primary})
			a.center = newCenter
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			newTerminal, cmd := a.sidebarTerminal.Update(messages.WorkspaceDeleted{Workspace: &msg.Workspace.Primary})
			a.sidebarTerminal = newTerminal
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Clean up the persisted tab state for the Primary workspace
			_ = a.workspaces.Delete(msg.Workspace.Primary.ID())
		}
		cmds = append(cmds, a.toast.ShowSuccess("Group workspace deleted"))
		cmds = append(cmds, a.loadGroups())

	case messages.GroupWorkspaceDeleteFailed:
		errMsg := "Failed to delete group workspace"
		if msg.Err != nil {
			errMsg += ": " + msg.Err.Error()
		}
		cmds = append(cmds, a.toast.ShowError(errMsg))

	case messages.GroupWorkspaceActivated:
		cmds = append(cmds, a.handleGroupWorkspaceActivated(msg)...)

	case messages.GroupWorkspacePreviewed:
		cmds = append(cmds, a.handleGroupWorkspacePreviewed(msg)...)

	case messages.ShowSetGroupProfileDialog:
		if a.groupHasActiveSessions(msg.Group) {
			cmds = append(cmds, a.toast.ShowError("Cannot change profile while workspaces have active sessions"))
			break
		}
		a.handleShowSetGroupProfileDialog(msg)

	case messages.SetGroupProfile:
		if cmd := a.handleSetGroupProfile(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.LaunchGroupAgent:
		if cmd := a.handleLaunchGroupAgent(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GroupPreviewed:
		cmds = append(cmds, a.handleGroupPreviewed(msg)...)

	case messages.ShowEditGroupReposDialog:
		if a.groupHasActiveSessions(msg.Group) {
			cmds = append(cmds, a.toast.ShowError("Cannot edit repos while workspaces have active sessions"))
			break
		}
		a.handleShowEditGroupReposDialog(msg.Group)

	case messages.UpdateGroupRepos:
		if cmd := a.handleUpdateGroupRepos(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GroupReposUpdated:
		cmds = append(cmds, a.toast.ShowSuccess("Group repos updated"))
		cmds = append(cmds, a.loadGroups())

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
