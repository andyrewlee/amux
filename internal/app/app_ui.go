package app

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	case messages.PaneSidebarTerminal:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Focus()
		// Lazy initialization: create terminal on focus if none exists
		return a.sidebarTerminal.EnsureTerminalTab()
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
	switch {
	// Pane focus
	case key.Matches(msg, a.keymap.MoveLeft):
		switch a.focusedPane {
		case messages.PaneCenter:
			a.focusPane(messages.PaneDashboard)
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
			a.focusPane(messages.PaneCenter)
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveRight):
		switch a.focusedPane {
		case messages.PaneDashboard:
			a.focusPane(messages.PaneCenter)
		case messages.PaneCenter:
			if a.layout.ShowSidebar() {
				a.focusPane(messages.PaneSidebar)
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveUp):
		if a.focusedPane == messages.PaneSidebarTerminal {
			a.focusPane(messages.PaneSidebar)
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveDown):
		if a.focusedPane == messages.PaneSidebar && a.layout.ShowSidebar() {
			cmd := a.focusPane(messages.PaneSidebarTerminal)
			return true, cmd
		}
		return true, nil

	// Tab management - route to appropriate pane
	case key.Matches(msg, a.keymap.NextTab):
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.NextTab()
		case messages.PaneSidebar:
			a.sidebar.NextTab()
		default:
			_, activeIdxBefore := a.center.GetTabsInfo()
			a.center.NextTab()
			_, activeIdxAfter := a.center.GetTabsInfo()
			if activeIdxAfter == activeIdxBefore {
				return true, nil
			}
			return true, common.SafeBatch(a.center.ReattachActiveTab(), a.persistActiveWorkspaceTabs())
		}
		return true, nil

	case key.Matches(msg, a.keymap.PrevTab):
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.PrevTab()
		case messages.PaneSidebar:
			a.sidebar.PrevTab()
		default:
			_, activeIdxBefore := a.center.GetTabsInfo()
			a.center.PrevTab()
			_, activeIdxAfter := a.center.GetTabsInfo()
			if activeIdxAfter == activeIdxBefore {
				return true, nil
			}
			return true, common.SafeBatch(a.center.ReattachActiveTab(), a.persistActiveWorkspaceTabs())
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
		if a.focusedPane == messages.PaneSidebarTerminal && a.activeWorkspace != nil {
			if !a.tmuxAvailable {
				return true, a.toast.ShowError("tmux required to create tabs. " + a.tmuxInstallHint)
			}
			return true, a.sidebarTerminal.CreateNewTab()
		}
		return true, nil

	case key.Matches(msg, a.keymap.CloseTab):
		if a.focusedPane == messages.PaneSidebarTerminal {
			return true, a.sidebarTerminal.CloseActiveTab()
		}
		cmd := a.center.CloseActiveTab()
		return true, cmd

	case key.Matches(msg, a.keymap.DetachTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.DetachActiveTab()
			return true, common.SafeBatch(cmd, a.persistActiveWorkspaceTabs())
		case messages.PaneSidebarTerminal:
			if cmd := a.sidebarTerminal.DetachActiveTab(); cmd != nil {
				return true, cmd
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.ReattachTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.ReattachActiveTab()
			return true, cmd
		case messages.PaneSidebarTerminal:
			if cmd := a.sidebarTerminal.ReattachActiveTab(); cmd != nil {
				return true, cmd
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.RestartTab):
		switch a.focusedPane {
		case messages.PaneCenter:
			cmd := a.center.RestartActiveTab()
			return true, cmd
		case messages.PaneSidebarTerminal:
			if cmd := a.sidebarTerminal.RestartActiveTab(); cmd != nil {
				return true, cmd
			}
		}
		return true, nil

	case key.Matches(msg, a.keymap.CleanupTmux):
		return true, func() tea.Msg { return messages.ShowCleanupTmuxDialog{} }

	// Global commands
	case key.Matches(msg, a.keymap.Help):
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return true, nil

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
			tabs, activeIdx := a.center.GetTabsInfo()
			if index < 0 || index >= len(tabs) || index == activeIdx {
				return true, nil
			}
			a.center.SelectTab(index)
			return true, common.SafeBatch(a.center.ReattachActiveTab(), a.persistActiveWorkspaceTabs())
		}
	}

	return false, nil
}

// sendPrefixToTerminal sends a literal Ctrl-Space (NUL) to the focused terminal
func (a *App) sendPrefixToTerminal() {
	if a.focusedPane == messages.PaneCenter {
		a.center.SendToTerminal("\x00")
	} else if a.focusedPane == messages.PaneSidebarTerminal {
		a.sidebarTerminal.SendToTerminal("\x00")
	}
}

// updateLayout updates component sizes based on window size
func (a *App) updateLayout() {
	a.dashboard.SetSize(a.layout.DashboardWidth(), a.layout.Height())

	centerWidth := a.layout.CenterWidth()
	a.center.SetSize(centerWidth, a.layout.Height())
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	a.center.SetOffset(leftGutter + a.layout.DashboardWidth() + gapX) // Set X offset for mouse coordinate conversion
	a.center.SetCanFocusRight(a.layout.ShowSidebar())
	a.dashboard.SetCanFocusRight(a.layout.ShowCenter())

	// New two-pane sidebar structure: each pane has its own border
	sidebarWidth := a.layout.SidebarWidth()
	sidebarHeight := a.layout.Height()

	// Each pane gets half the height (borders touch)
	topPaneHeight, bottomPaneHeight := sidebarPaneHeights(sidebarHeight)

	// Content dimensions inside each pane (subtract border + padding)
	// Border: 2 (top + bottom), Padding: 2 (left + right from Pane style)
	contentWidth := sidebarWidth - 2 - 2 // border + padding
	if contentWidth < 1 {
		contentWidth = 1
	}
	topContentHeight := topPaneHeight - 2 // border only (no vertical padding in Pane style)
	if topContentHeight < 1 {
		topContentHeight = 1
	}
	bottomContentHeight := bottomPaneHeight - 2
	if bottomContentHeight < 1 {
		bottomContentHeight = 1
	}

	a.sidebar.SetSize(contentWidth, topContentHeight)
	a.sidebarTerminal.SetSize(contentWidth, bottomContentHeight)

	// Calculate and set offsets for sidebar mouse handling
	// X: Dashboard + Center + Border(1) + Padding(1)
	sidebarX := leftGutter + a.layout.DashboardWidth()
	if a.layout.ShowCenter() {
		sidebarX += a.layout.GapX() + a.layout.CenterWidth()
	}
	if a.layout.ShowSidebar() {
		sidebarX += a.layout.GapX()
	}
	sidebarContentOffsetX := sidebarX + 2 // +2 for border and padding

	// Y: Top pane height (including its border) + Bottom pane border(1)
	termOffsetY := topGutter + topPaneHeight + 1
	a.sidebarTerminal.SetOffset(sidebarContentOffsetX, termOffsetY)

	if a.dialog != nil {
		a.dialog.SetSize(a.width, a.height)
	}
	if a.filePicker != nil {
		a.filePicker.SetSize(a.width, a.height)
	}
	if a.settingsDialog != nil {
		a.settingsDialog.SetSize(a.width, a.height)
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

func sidebarPaneHeights(total int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	top := total / 2
	bottom := total - top

	// Prefer keeping both panes visible when there's room.
	if total >= 6 {
		if top < 3 {
			top = 3
			bottom = total - top
		}
		if bottom < 3 {
			bottom = 3
			top = total - bottom
		}
		return top, bottom
	}

	// In tight spaces, keep the terminal visible by shrinking the top pane first.
	if total >= 3 && bottom < 3 {
		bottom = 3
		top = total - bottom
		if top < 0 {
			top = 0
		}
		return top, bottom
	}

	if top > total {
		top = total
	}
	if bottom < 0 {
		bottom = 0
	}
	return top, bottom
}
