package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// setFocusedPane updates pane focus state without triggering pane-specific side effects.
func (a *App) setFocusedPane(pane messages.PaneType) {
	a.focusedPane = pane
	// Keep focus transitions fail-safe for partially initialized App instances
	// used in lightweight tests.
	a.syncPaneFocusFlags()
}

// focusPane changes focus to the specified pane
func (a *App) focusPane(pane messages.PaneType) tea.Cmd {
	a.setFocusedPane(pane)
	switch pane {
	case messages.PaneCenter:
		// Seamless UX: when center regains focus, attempt reattach for detached active tab.
		if a.center != nil {
			return a.center.ReattachActiveTabIfDetached()
		}
	case messages.PaneSidebarTerminal:
		// Lazy initialization: create terminal on focus if none exists.
		if a.sidebarTerminal != nil {
			return a.sidebarTerminal.EnsureTerminalTab()
		}
	}
	return nil
}

// focusPaneOnWheel updates focus for hover-wheel routing and preserves only the
// center-pane detached-tab reattach behavior. It intentionally skips other
// focus-time side effects such as lazy sidebar terminal creation.
func (a *App) focusPaneOnWheel(pane messages.PaneType) tea.Cmd {
	a.setFocusedPane(pane)
	if pane == messages.PaneCenter && a.center != nil {
		return a.center.ReattachActiveTabIfDetached()
	}
	return nil
}

// focusPaneLeft moves focus one pane to the left, respecting layout visibility.
func (a *App) focusPaneLeft() tea.Cmd {
	switch a.focusedPane {
	case messages.PaneSidebarTerminal, messages.PaneSidebar:
		if a.layout != nil && a.layout.ShowCenter() {
			return a.focusPane(messages.PaneCenter)
		}
		return a.focusPane(messages.PaneDashboard)
	case messages.PaneCenter:
		return a.focusPane(messages.PaneDashboard)
	}
	return nil
}

// focusPaneRight moves focus one pane to the right, respecting layout visibility.
func (a *App) focusPaneRight() tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		if a.layout != nil && a.layout.ShowCenter() {
			return a.focusPane(messages.PaneCenter)
		}
		if a.layout != nil && a.layout.ShowSidebar() {
			return a.focusPane(messages.PaneSidebar)
		}
	case messages.PaneCenter:
		if a.layout != nil && a.layout.ShowSidebar() {
			return a.focusPane(messages.PaneSidebar)
		}
	}
	return nil
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
