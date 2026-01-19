package app

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// focusPane changes focus to the specified pane
func (a *App) focusPane(pane messages.PaneType) {
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
	case messages.PaneMonitor:
		a.dashboard.Blur()
		a.center.Blur()
		a.sidebar.Blur()
		a.sidebarTerminal.Blur()
	}
}

func (a *App) toggleMonitorMode() {
	a.monitorMode = !a.monitorMode
	if a.monitorMode {
		a.center.ResetMonitorSelection()
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneMonitor)
	} else {
		a.monitorLayoutKey = ""
		a.focusPane(messages.PaneDashboard)
	}
	a.updateLayout()
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
	return tea.Tick(prefixTimeout, func(t time.Time) tea.Msg {
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
		case messages.PaneCenter:
			a.focusPane(messages.PaneDashboard)
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
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
		case messages.PaneCenter:
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
		if a.focusedPane == messages.PaneSidebarTerminal {
			a.focusPane(messages.PaneSidebar)
		}
		return true, nil

	case key.Matches(msg, a.keymap.MoveDown):
		if a.focusedPane == messages.PaneMonitor {
			// Move selection down in grid
			moveMonitorTile(0, 1)
			return true, nil
		}
		if a.focusedPane == messages.PaneSidebar && a.layout.ShowSidebar() {
			a.focusPane(messages.PaneSidebarTerminal)
		}
		return true, nil

	// Tab management
	case key.Matches(msg, a.keymap.NextTab):
		a.center.NextTab()
		return true, nil

	case key.Matches(msg, a.keymap.PrevTab):
		a.center.PrevTab()
		return true, nil

	// Tab management
	case key.Matches(msg, a.keymap.NewAgentTab):
		if a.activeWorktree != nil {
			return true, func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
		return true, nil

	case key.Matches(msg, a.keymap.CommitViewer):
		if a.activeWorktree != nil {
			wt := a.activeWorktree
			return true, func() tea.Msg { return messages.OpenCommitViewer{Worktree: wt} }
		}
		return true, nil

	case key.Matches(msg, a.keymap.CloseTab):
		cmd := a.center.CloseActiveTab()
		return true, cmd

	// Global commands
	case key.Matches(msg, a.keymap.Monitor):
		a.toggleMonitorMode()
		return true, nil

	case key.Matches(msg, a.keymap.Help):
		a.helpOverlay.SetSize(a.width, a.height)
		a.helpOverlay.Toggle()
		return true, nil

	case key.Matches(msg, a.keymap.Quit):
		a.showQuitDialog()
		return true, nil

	// Copy mode (scroll in terminal) - targets focused pane
	case key.Matches(msg, a.keymap.CopyMode):
		switch a.focusedPane {
		case messages.PaneCenter:
			a.center.EnterCopyMode()
		case messages.PaneSidebarTerminal:
			a.sidebarTerminal.EnterCopyMode()
		}
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
			return true, nil
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
	if a.monitorMode && a.layout.ShowCenter() {
		centerWidth += a.layout.SidebarWidth()
	}
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

	// Calculate and set offsets for sidebar terminal mouse handling
	// X: Dashboard + Center + Border(1) + Padding(1)
	sidebarX := leftGutter + a.layout.DashboardWidth()
	if a.layout.ShowCenter() {
		sidebarX += a.layout.GapX() + a.layout.CenterWidth()
	}
	if a.layout.ShowSidebar() {
		sidebarX += a.layout.GapX()
	}
	termOffsetX := sidebarX + 2

	// Y: Top pane height (including its border) + Bottom pane border(1)
	termOffsetY := topGutter + topPaneHeight + 1
	a.sidebarTerminal.SetOffset(termOffsetX, termOffsetY)

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

func (a *App) handleCenterPaneClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if a.layout == nil || !a.layout.ShowCenter() || a.center.HasTabs() {
		return nil
	}
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := a.layout.GapX()
	if centerWidth <= 0 {
		return nil
	}
	centerStart := a.layout.LeftGutter() + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	if msg.X < centerStart || msg.X >= centerEnd {
		return nil
	}
	contentX, contentY := a.centerPaneContentOrigin()
	localX := msg.X - contentX
	localY := msg.Y - contentY
	if localX < 0 || localY < 0 {
		return nil
	}

	if a.showWelcome {
		return a.handleWelcomeClick(localX, localY)
	}
	if a.activeWorktree != nil {
		return a.handleWorktreeInfoClick(localX, localY)
	}
	return nil
}

func (a *App) handleWelcomeClick(localX, localY int) tea.Cmd {
	content := a.welcomeContent()
	lines := strings.Split(content, "\n")
	contentWidth, contentHeight := viewDimensions(content)

	// Match the width/height used by renderWelcome for centering.
	placeWidth := a.layout.CenterWidth() - 4
	placeHeight := a.layout.Height() - 4
	if placeWidth <= 0 || placeHeight <= 0 {
		return nil
	}

	offsetX := centerOffset(placeWidth, contentWidth)
	offsetY := centerOffset(placeHeight, contentHeight)

	// Both buttons are on the same line, find them by searching for plain text
	for i, line := range lines {
		strippedLine := ansi.Strip(line)

		// Settings button - check first so it's not blocked by New project's region
		settingsText := "Settings"
		if idx := strings.Index(strippedLine, settingsText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + offsetX,
				Y:      i + offsetY,
				Width:  len(settingsText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSettingsDialog{} }
			}
		}

		// New project button
		newProjectText := "New project"
		if idx := strings.Index(strippedLine, newProjectText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + offsetX,
				Y:      i + offsetY,
				Width:  len(newProjectText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowAddProjectDialog{} }
			}
		}
	}

	return nil
}

func (a *App) handleWorktreeInfoClick(localX, localY int) tea.Cmd {
	if a.activeWorktree == nil {
		return nil
	}
	content := a.renderWorktreeInfo()
	lines := strings.Split(content, "\n")

	agentBtn := a.styles.TabPlus.Render("New agent")
	if region, ok := findButtonRegion(lines, agentBtn); ok {
		if region.Contains(localX, localY) {
			return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
		}
	}

	commitsBtn := a.styles.TabPlus.Render("Commits")
	if region, ok := findButtonRegion(lines, commitsBtn); ok {
		if region.Contains(localX, localY) {
			wt := a.activeWorktree
			return func() tea.Msg { return messages.OpenCommitViewer{Worktree: wt} }
		}
	}

	return nil
}
