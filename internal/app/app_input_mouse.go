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
	inSidebarX := a.layout.ShowSidebar() && msg.X >= sidebarStart && msg.X < sidebarEnd
	localY := msg.Y - topGutter

	// Focus pane on left-click press
	if msg.Button == tea.MouseLeft {
		if msg.X < leftGutter {
			a.focusPane(messages.PaneDashboard)
		} else if msg.X < leftGutter+dashWidth {
			// Clicked on dashboard (left bar)
			a.focusPane(messages.PaneDashboard)
		} else if msg.X < centerEnd {
			// Clicked on center pane
			a.focusPane(messages.PaneCenter)
		} else if inSidebarX {
			// Clicked on sidebar - determine top (changes) or bottom (terminal)
			sidebarHeight := a.layout.Height()
			topPaneHeight, _ := sidebarPaneHeights(sidebarHeight)

			// Split point is after top pane
			if localY >= topPaneHeight {
				a.focusPane(messages.PaneSidebarTerminal)
			} else {
				a.focusPane(messages.PaneSidebar)
			}
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
	case messages.PaneSidebarTerminal:
		// Ignore clicks in the gap/right gutter so they don't trigger sidebar actions.
		if inSidebarX {
			newTerm, cmd := a.sidebarTerminal.Update(msg)
			a.sidebarTerminal = newTerm
			return cmd
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
	return nil
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
	case messages.PaneSidebarTerminal:
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
	case messages.PaneSidebarTerminal:
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
	case messages.PaneSidebarTerminal:
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
func (a *App) handleMonitorModeClick(msg tea.MouseClickMsg) bool {
	if msg.Button == tea.MouseLeft {
		a.focusPane(messages.PaneMonitor)
		if a.monitorExitHit(msg.X, msg.Y) {
			a.toggleMonitorMode()
			return true
		}
		if filter, ok := a.monitorFilterHit(msg.X, msg.Y); ok {
			a.monitorFilter = filter
			return true
		}
		// Click to focus tile (just select, don't exit)
		a.selectMonitorTile(msg.X, msg.Y)
	}
	return true
}
