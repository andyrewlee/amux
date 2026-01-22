package app

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// Update handles all messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case DialogAddProject, DialogCreateWorktree, DialogDeleteWorktree, DialogRemoveProject, DialogSelectAssistant, "agent-picker", DialogQuit:
			return a, a.handleDialogResult(result)
		}
		// If not an App-level dialog, let it fall through to components
		// Currently only Center uses custom dialogs
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return a, cmd
	}

	// Handle help overlay input (highest priority when visible)
	if a.helpOverlay.Visible() {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			var cmd tea.Cmd
			a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
			return a, cmd
		case tea.MouseWheelMsg:
			a.helpOverlay, _, _ = a.helpOverlay.Update(msg)
			return a, nil
		case tea.MouseClickMsg:
			if msg.Button == tea.MouseLeft {
				// First check if clicking on a link inside the dialog
				var cmd tea.Cmd
				a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
				if cmd != nil {
					return a, cmd
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
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
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
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.PasteMsg); ok {
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
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
			return a, tea.Batch(cmds...)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			return a, tea.Batch(cmds...)
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
			a.handleMonitorModeClick(msg)
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

	case messages.WorktreeActivated:
		cmds = append(cmds, a.handleWorktreeActivated(msg)...)

	case messages.ShowWelcome:
		a.goHome()

	case messages.ToggleMonitor:
		a.toggleMonitorMode()

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

	case messages.WorktreeCreatedWithWarning:
		cmds = append(cmds, a.handleWorktreeCreatedWithWarning(msg)...)

	case messages.WorktreeCreated:
		cmds = append(cmds, a.handleWorktreeCreated(msg)...)

	case messages.WorktreeCreateFailed:
		if cmd := a.handleWorktreeCreateFailed(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GitStatusResult:
		if cmd := a.handleGitStatusResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.ShowAddProjectDialog:
		a.handleShowAddProjectDialog()

	case messages.ShowCreateWorktreeDialog:
		a.handleShowCreateWorktreeDialog(msg)

	case messages.ShowDeleteWorktreeDialog:
		a.handleShowDeleteWorktreeDialog(msg)

	case messages.ShowRemoveProjectDialog:
		a.handleShowRemoveProjectDialog(msg)

	case messages.ShowSelectAssistantDialog:
		a.handleShowSelectAssistantDialog()

	case messages.ShowSettingsDialog:
		a.handleShowSettingsDialog()

	case common.ThemePreview:
		a.handleThemePreview(msg)

	case common.SettingsResult:
		if cmd := a.handleSettingsResult(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.CreateWorktree:
		cmds = append(cmds, a.handleCreateWorktree(msg)...)

	case messages.DeleteWorktree:
		cmds = append(cmds, a.handleDeleteWorktree(msg)...)

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

	case messages.TabClosed:
		logging.Info("Tab closed: %d", msg.Index)

	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		if cmd := a.handlePTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, sidebar.SidebarTerminalCreated:
		if cmd := a.handleSidebarPTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GitStatusTick:
		cmds = append(cmds, a.handleGitStatusTick()...)

	case messages.FileWatcherEvent:
		cmds = append(cmds, a.handleFileWatcherEvent(msg)...)

	case messages.WorktreeDeleted:
		cmds = append(cmds, a.handleWorktreeDeleted(msg)...)

	case messages.ProjectRemoved:
		cmds = append(cmds, a.toast.ShowSuccess("Project removed"))
		cmds = append(cmds, a.loadProjects())

	case messages.WorktreeDeleteFailed:
		if cmd := a.handleWorktreeDeleteFailed(msg); cmd != nil {
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

	return a, tea.Batch(cmds...)
}

// handleKeyPress handles keyboard input
func (a *App) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	// Dismiss error on any key
	if a.err != nil {
		a.err = nil
		return nil
	}

	// 1. Handle prefix key (Ctrl+Space)
	if a.isPrefixKey(msg) {
		if a.prefixActive {
			// Prefix + Prefix = send literal Ctrl+Space to terminal
			a.sendPrefixToTerminal()
			a.exitPrefix()
			return nil
		}
		// Enter prefix mode
		return a.enterPrefix()
	}

	// 2. If prefix is active, handle mux commands
	if a.prefixActive {
		// Esc cancels prefix mode without forwarding
		code := msg.Key().Code
		if code == tea.KeyEsc || code == tea.KeyEscape {
			a.exitPrefix()
			return nil
		}

		handled, cmd := a.handlePrefixCommand(msg)
		if handled {
			a.exitPrefix()
			return cmd
		}
		// Unknown key in prefix mode: exit prefix and pass through
		a.exitPrefix()
		// Fall through to normal handling below
	}

	// 3. Passthrough mode - route keys to focused pane
	// Monitor pane: all keys go to the selected tile's PTY (navigation is via prefix mode)
	if a.focusedPane == messages.PaneMonitor {
		if cmd := a.handleMonitorInput(msg); cmd != nil {
			return cmd
		}
		return nil
	}

	// Handle button navigation when center pane is focused and showing welcome/worktree info (no tabs)
	if a.focusedPane == messages.PaneCenter && !a.center.HasTabs() {
		maxIndex := a.centerButtonCount() - 1
		switch {
		case key.Matches(msg, a.keymap.Left), key.Matches(msg, a.keymap.Up):
			if a.centerBtnFocused {
				if a.centerBtnIndex > 0 {
					a.centerBtnIndex--
				} else {
					a.centerBtnFocused = false
				}
			} else {
				// Enter from the right/bottom - focus last button
				a.centerBtnFocused = true
				a.centerBtnIndex = maxIndex
			}
			return nil
		case key.Matches(msg, a.keymap.Right), key.Matches(msg, a.keymap.Down):
			if a.centerBtnFocused {
				if a.centerBtnIndex < maxIndex {
					a.centerBtnIndex++
				} else {
					a.centerBtnFocused = false
				}
			} else {
				// Enter from the left/top - focus first button
				a.centerBtnFocused = true
				a.centerBtnIndex = 0
			}
			return nil
		case key.Matches(msg, a.keymap.Enter):
			if a.centerBtnFocused {
				return a.activateCenterButton()
			}
		}
	}

	// Route to focused pane
	switch a.focusedPane {
	case messages.PaneDashboard:
		newDashboard, cmd := a.dashboard.Update(msg)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		return cmd
	case messages.PaneSidebar:
		newSidebar, cmd := a.sidebar.Update(msg)
		a.sidebar = newSidebar
		return cmd
	case messages.PaneSidebarTerminal:
		newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newSidebarTerminal
		return cmd
	}
	return nil
}

// centerButtonCount returns the number of buttons shown on the current center screen
func (a *App) centerButtonCount() int {
	if a.showWelcome {
		return 2 // [New project], [Settings]
	}
	if a.activeWorktree != nil {
		return 1 // [New agent]
	}
	return 0
}

// activateCenterButton activates the currently focused center button
func (a *App) activateCenterButton() tea.Cmd {
	if a.showWelcome {
		switch a.centerBtnIndex {
		case 0:
			return func() tea.Msg { return messages.ShowAddProjectDialog{} }
		case 1:
			return func() tea.Msg { return messages.ShowSettingsDialog{} }
		}
	} else if a.activeWorktree != nil {
		return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
	}
	return nil
}
