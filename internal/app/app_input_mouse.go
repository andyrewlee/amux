package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// routeMouseClick routes mouse click events to the appropriate pane.
func (a *App) routeMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	centerStart := leftGutter + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	sidebarStart := centerEnd
	sidebarEnd := centerEnd
	if a.layout.ShowSidebar() {
		sidebarStart = centerEnd + gapX
		sidebarEnd = sidebarStart + a.layout.SidebarWidth()
	}

	// Terminal pane is below center, spanning center's width
	terminalStart := centerStart
	terminalEnd := centerEnd
	terminalTopY := topGutter + a.layout.CenterContentHeight()
	terminalBottomY := terminalTopY + a.layout.TerminalHeight()

	inSidebarX := a.layout.ShowSidebar() && msg.X >= sidebarStart && msg.X < sidebarEnd
	inTerminalArea := a.layout.ShowTerminal() && msg.X >= terminalStart && msg.X < terminalEnd && msg.Y >= terminalTopY && msg.Y < terminalBottomY
	inCenterArea := a.layout.ShowCenter() && msg.X >= centerStart && msg.X < centerEnd && msg.Y >= topGutter && msg.Y < terminalTopY

	// Focus pane on left-click press
	var focusCmd tea.Cmd
	if msg.Button == tea.MouseLeft {
		// Check for terminal toggle button click
		if inTerminalArea && msg.X == a.terminalToggleX && msg.Y == a.terminalToggleY {
			return func() tea.Msg { return messages.ToggleTerminalCollapse{} }
		}

		if msg.X < leftGutter {
			a.focusPane(messages.PaneDashboard)
		} else if msg.X < leftGutter+dashWidth {
			// Clicked on dashboard (left bar)
			a.focusPane(messages.PaneDashboard)
		} else if inTerminalArea {
			// Clicked on terminal pane (below center)
			focusCmd = a.focusPane(messages.PaneTerminal)
		} else if inCenterArea {
			// Clicked on center pane
			a.focusPane(messages.PaneCenter)
		} else if inSidebarX {
			// Clicked on sidebar (git changes)
			a.focusPane(messages.PaneSidebar)
		}
	}

	if cmd := a.handleCenterPaneClick(msg); cmd != nil {
		return cmd
	}

	// Forward mouse events to the focused pane
	// This ensures drag events are received even if the mouse leaves the pane bounds
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		// Forward clicks to terminal pane
		if inTerminalArea {
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			// If the click returned a command (e.g., CreateNewTab from "+ New" button),
			// skip focusCmd to avoid double terminal creation
			if cmd != nil {
				return cmd
			}
			return focusCmd
		}
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		// Ignore clicks in the gap/right gutter so they don't trigger sidebar actions.
		if inSidebarX {
			newSidebar, cmd := a.sidebar.Update(adjusted)
			a.sidebar = newSidebar
			return cmd
		}
	}
	return focusCmd
}

// routeMouseWheel routes mouse wheel events to the appropriate pane.
func (a *App) routeMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// routeMouseMotion routes mouse motion events to the appropriate pane.
func (a *App) routeMouseMotion(msg tea.MouseMotionMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// routeMouseRelease routes mouse release events to the appropriate pane.
func (a *App) routeMouseRelease(msg tea.MouseReleaseMsg) tea.Cmd {
	switch a.focusedPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return cmd
	case messages.PaneTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return cmd
	}
	return nil
}

// handleMonitorModeClick handles mouse clicks in monitor mode.
func (a *App) handleMonitorModeClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button == tea.MouseLeft {
		a.focusPane(messages.PaneMonitor)
		if a.monitorExitHit(msg.X, msg.Y) {
			return a.toggleMonitorMode()
		}
		if filter, ok := a.monitorFilterHit(msg.X, msg.Y); ok {
			a.monitorFilter = filter
			return nil
		}
		// Click to focus tile (just select, don't exit)
		a.selectMonitorTile(msg.X, msg.Y)
	}
	return nil
}
