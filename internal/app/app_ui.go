package app

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// focusPane changes focus to the specified pane
func (a *App) focusPane(pane messages.PaneType) tea.Cmd {
	a.focusedPane = pane
	switch pane {
	case messages.PaneDashboard:
		a.dashboard.Focus()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	case messages.PaneCenter:
		a.dashboard.Blur()
		a.center.Focus()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	case messages.PaneSidebar:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Focus()
		a.sidebarTerminal.Blur()
	case messages.PaneTerminal:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Focus()
		// Lazy initialization: create terminal on focus if none exists
		return a.sidebarTerminal.EnsureTerminalTab()
	case messages.PaneMonitor:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	}
	return nil
}

func (a *App) toggleMonitorMode() tea.Cmd {
	a.monitorMode = !a.monitorMode
	if a.monitorMode {
		a.center.ResetMonitorSelection()
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneMonitor)
	} else {
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneDashboard)
	}
	a.center.SetMonitorMode(a.monitorMode)
	a.updateLayout()
	if a.monitorMode {
		return a.center.StartMonitorSnapshots()
	}
	return nil
}

// Prefix mode helpers (leader key)

// isPrefixKey returns true if the key is the prefix key
func (a *App) isPrefixKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, a.keymap.Prefix)
}

// enterPrefix enters prefix mode and schedules a timeout
func (a *App) enterPrefix() tea.Cmd {
	a.prefixActive = true
	a.prefixToken++
	token := a.prefixToken
	return common.SafeTick(prefixTimeout, func(t time.Time) tea.Msg {
		return prefixTimeoutMsg{token: token}
	})
}

// exitPrefix exits prefix mode
func (a *App) exitPrefix() {
	a.prefixActive = false
}

// handlePrefixCommand handles a key press while in prefix mode
// Returns (handled, cmd)
func (a *App) handlePrefixCommand(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// Helper to move monitor selection
	moveMonitorTile := func(dx, dy int) {
		tabs := a.center.MonitorTabs()
		if len(tabs) == 0 {
			return
		}
		_, _, gridW, gridH := a.monitorGridArea()
		grid := monitorGridLayout(len(tabs), gridW, gridH)
		if grid.cols > 0 && grid.rows > 0 {
			a.center.MoveMonitorSelection(dx, dy, grid.cols, grid.rows, len(tabs))
		}
	}

	switch {
	// Pane focus / Monitor tile navigation
	case key.Matches(msg, a.keymap.MoveLeft):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection left in grid (like pane focus in normal mode)
			moveMonitorTile(-1, 0)
			return true, nil
		}
		switch a.focusedPane {
		case messages.PaneCenter, messages.PaneTerminal:
			a.focusPane(messages.PaneDashboard)
		case messages.PaneSidebar:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveRight):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection right in grid (like pane focus in normal mode)
			moveMonitorTile(1, 0)
			return true, nil
		}
		switch a.focusedPane {
		case messages.PaneDashboard:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		case messages.PaneCenter, messages.PaneTerminal:
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else if a.layout.ShowSidebar() {
				a.focusPane(messages.PaneSidebar)
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveUp):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection up in grid
			moveMonitorTile(0, -1)
			return true, nil
		}
		if a.focusedPane == messages.PaneTerminal && a.layout.ShowTerminal() {
			a.focusPane(messages.PaneCenter)
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveDown):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection down in grid
			moveMonitorTile(0, 1)
			return true, nil
		}
		if a.focusedPane == messages.PaneCenter && a.layout.ShowTerminal() {
			cmd := a.focusPane(messages.PaneTerminal)
			return true, cmd
		}
		return true, nil

	// Tab management - route to appropriate pane
	case key.Matches(msg, a.keymap.NextTab):
		switch a.focusedPane {
		case messages.PaneTerminal:
			a.sidebarTerminal.NextTab()
		case messages.PaneSidebar:
			a.sidebar.NextTab()
		default:
			a.center.NextTab()
			return true, a.persistActiveWorkspaceTabs()
		}
		return true, nil

	case key.Matches(msg, a.keymap.PrevTab):
		switch a.focusedPane {
		case messages.PaneTerminal:
			a.sidebarTerminal.PrevTab()
		case messages.PaneSidebar:
			a.sidebar.PrevTab()
		default:
			a.center.PrevTab()
			return true, a.persistActiveWorkspaceTabs()
		}
		return true, nil

	// Tab management
	case key.Matches(msg, a.keymap.NewAgentTab):
		if a.activeWorkspace != nil {
			if !a.tmuxAvailable {
				return true, a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
			}
			return true, func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
		return true, nil

	case key.Matches(msg, a.keymap.NewTerminalTab):
		if a.focusedPane == messages.PaneTerminal && a.activeWorkspace != nil {
			return true, a.sidebarTerminal.CreateNewTab()
		}
		return true, nil

	case key.Matches(msg, a.keymap.CloseTab):
		if a.focusedPane == messages.PaneTerminal {
			return true, a.sidebarTerminal.CloseActiveTab()
		}
		cmd := a.center.CloseActiveTab()
		return true, cmd

	case key.Matches(msg, a.keymap.DetachTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.DetachActiveTab()
			return true, a.safeBatch(cmd, a.persistActiveWorkspaceTabs())
		}
		return true, nil

	case key.Matches(msg, a.keymap.ReattachTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.ReattachActiveTab()
			return true, cmd
		}
		return true, nil

	case key.Matches(msg, a.keymap.RestartTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.RestartActiveTab()
			return true, cmd
		case messages.PaneTerminal:
			if cmd := a.sidebarTerminal.RestartActiveTab(); cmd != nil {
				return true, cmd
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.CleanupTmux):
		return true, func() tea.Msg { return messages.ShowCleanupTmuxDialog{} }

	// Global commands
	case key.Matches(msg, a.keymap.Monitor):
		return true, a.toggleMonitorMode()

	case key.Matches(msg, a.keymap.Help):
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return true, nil

	case key.Matches(msg, a.keymap.GlobalPerms):
		if !a.config.UI.GlobalPermissions {
			return true, a.toast.ShowInfo("Global permissions is disabled")
		}
		if len(a.pendingPermissions) == 0 {
			return true, a.toast.ShowInfo("No pending permissions to review")
		}
		return true, func() tea.Msg { return messages.ShowPermissionsDialog{} }

	case key.Matches(msg, a.keymap.Quit):
		a.showQuitDialog()
		return true, nil

	// Tab numbers 1-9
	case len(msg.Key().Text) > 0:
		runes := []rune(msg.Key().Text)
		if len(runes) != 1 {
			break
		}
		r := runes[0]
		if r >= '1' && r <= '9' {
			index := int(r - '1')
			a.center.SelectTab(index)
			return true, a.persistActiveWorkspaceTabs()
		}
	}

	return false, nil
}

// sendPrefixToTerminal sends a literal Ctrl-A to the focused terminal
func (a *App) sendPrefixToTerminal() {
	if a.focusedPane == messages.PaneCenter {
		a.center.SendToTerminal("\x01")
	} else if a.focusedPane == messages.PaneTerminal {
		a.sidebarTerminal.SendToTerminal("\x01")
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())

	centerWidth := a.layout.CenterWidth()
	centerHeight := a.layout.CenterContentHeight() // Height minus terminal
	if a.monitorMode && a.layout.ShowCenter() {
		centerWidth += a.layout.SidebarWidth()
		centerHeight = a.layout.Height() // In monitor mode, center gets full height
	}
	a.center.SetSize(centerWidth, centerHeight)
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	centerX := leftGutter + a.layout.DashboardWidth() + gapX
	a.center.SetOffset(centerX) // Set X offset for mouse coordinate conversion
	a.center.SetCanFocusRight(a.layout.ShowSidebar())
	a.dashboard.SetCanFocusRight(a.layout.ShowCenter())

	// Sidebar is now a single full-height pane (git changes only)
	sidebarWidth := a.layout.SidebarWidth()
	sidebarHeight := a.layout.Height()

	// Content dimensions inside pane (subtract border + padding)
	// Border: 2 (top + bottom), Padding: 2 (left + right from Pane style)
	sidebarContentWidth := sidebarWidth - 2 - 2 // border + padding
	if sidebarContentWidth < 1 {
		sidebarContentWidth = 1
	}
	sidebarContentHeight := sidebarHeight - 2 // border only (no vertical padding in Pane style)
	if sidebarContentHeight < 1 {
		sidebarContentHeight = 1
	}

	a.sidebar.SetSize(sidebarContentWidth, sidebarContentHeight)

	// Terminal pane is now below center, spanning center's width
	terminalWidth := a.layout.CenterWidth()
	terminalHeight := a.layout.TerminalHeight()

	// Content dimensions inside terminal pane
	terminalContentWidth := terminalWidth - 2 - 2 // border + padding
	if terminalContentWidth < 1 {
		terminalContentWidth = 1
	}
	terminalContentHeight := terminalHeight - 2
	if terminalContentHeight < 1 {
		terminalContentHeight = 1
	}

	a.sidebarTerminal.SetSize(terminalContentWidth, terminalContentHeight)

	// Terminal position: below center pane
	terminalX := centerX
	terminalY := topGutter + a.layout.CenterContentHeight()
	terminalContentOffsetX := terminalX + 2 // +2 for border and padding
	terminalContentOffsetY := terminalY + 1 // +1 for top border
	a.sidebarTerminal.SetOffset(terminalContentOffsetX, terminalContentOffsetY)

	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
	if a.filePicker != nil {
		a.filePicker.SetSize(a.width, a.height)
	}
	if a.settingsDialog != nil {
		a.settingsDialog.SetSize(a.width, a.height)
	}
	if a.permissionsDialog != nil {
		a.permissionsDialog.SetSize(a.width, a.height)
	}
	if a.permissionsEditor != nil {
		a.permissionsEditor.SetSize(a.width, a.height)
	}
}

func (a *App) setKeymapHintsEnabled(enabled bool) {
	if a.config != nil {
		a.config.UI.ShowKeymapHints = enabled
	}
	a.dashboard.SetShowKeymapHints(enabled)
	a.center.SetShowKeymapHints(enabled)
	a.sidebar.SetShowKeymapHints(enabled)
	a.sidebarTerminal.SetShowKeymapHints(enabled)
	if a.dialog != nil {
		a.dialog.SetShowKeymapHints(enabled)
	}
	if a.filePicker != nil {
		a.filePicker.SetShowKeymapHints(enabled)
	}
}

