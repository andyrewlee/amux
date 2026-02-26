package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// routeMouseClick routes mouse click events to the appropriate pane.
func (a *App) routeMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	targetPane, hasTarget := a.paneForPoint(msg.X, msg.Y)

	// Focus pane on left-click press
	var focusCmd tea.Cmd
	if msg.Button == tea.MouseLeft && hasTarget {
		focusCmd = a.focusPane(targetPane)
	}

	if cmd := a.handleCenterPaneClick(msg); cmd != nil {
		return common.SafeBatch(focusCmd, cmd)
	}

	// Route click to the pane under the pointer to match web-style interaction.
	if !hasTarget {
		return focusCmd
	}

	switch targetPane {
	case messages.PaneDashboard:
		adjusted := msg
		if a.layout != nil {
			adjusted.X -= a.layout.LeftGutter()
			adjusted.Y -= a.layout.TopGutter()
		}
		newDashboard, cmd := a.dashboard.Update(adjusted)
		a.dashboard = newDashboard
		return common.SafeBatch(focusCmd, cmd)
	case messages.PaneCenter:
		adjusted := msg
		if a.layout != nil {
			adjusted.Y -= a.layout.TopGutter()
		}
		newCenter, cmd := a.center.Update(adjusted)
		a.center = newCenter
		return common.SafeBatch(focusCmd, cmd)
	case messages.PaneSidebarTerminal:
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		// If the click returned a command (e.g., CreateNewTab from "+ New" button),
		// skip focusCmd to avoid double terminal creation.
		if cmd != nil {
			return cmd
		}
		return focusCmd
	case messages.PaneSidebar:
		adjusted := msg
		if a.layout != nil {
			adjusted.X, adjusted.Y = a.adjustSidebarMouseXY(adjusted.X, adjusted.Y)
		}
		newSidebar, cmd := a.sidebar.Update(adjusted)
		a.sidebar = newSidebar
		return common.SafeBatch(focusCmd, cmd)
	}
	return focusCmd
}

func (a *App) handleMouseMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return a.routeMouseClick(msg)
	case tea.MouseWheelMsg:
		return a.routeMouseWheel(msg)
	case tea.MouseMotionMsg:
		return a.routeMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return a.routeMouseRelease(msg)
	default:
		return nil
	}
}

// routeMouseWheel routes mouse wheel events to the appropriate pane.
func (a *App) routeMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	// Preserve focused-pane wheel behavior so keyboard-selected panes continue
	// scrolling even when the pointer is elsewhere.
	targetPane := a.focusedPane
	switch targetPane {
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
	// Keep left-button drag motion bound to the pane focused on mouse-down.
	// Selection/edge-scroll logic depends on receiving out-of-bounds motion.
	targetPane := a.focusedPane
	if msg.Button != tea.MouseLeft {
		var ok bool
		targetPane, ok = a.paneForPoint(msg.X, msg.Y)
		if !ok {
			return nil
		}
	}
	switch targetPane {
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
	// Keep left-button release bound to the pane focused on mouse-down so
	// cross-pane drags still finalize selection state in the source pane.
	targetPane := a.focusedPane
	if msg.Button != tea.MouseLeft {
		var ok bool
		targetPane, ok = a.paneForPoint(msg.X, msg.Y)
		if !ok {
			return nil
		}
	}
	switch targetPane {
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

func (a *App) paneForPoint(x, y int) (messages.PaneType, bool) {
	if a.layout == nil {
		return messages.PaneCenter, false
	}
	topGutter := a.layout.TopGutter()
	height := a.layout.Height()
	if y < topGutter || y >= topGutter+height {
		return messages.PaneCenter, false
	}

	leftGutter := a.layout.LeftGutter()
	if x < leftGutter {
		return messages.PaneDashboard, true
	}

	dashWidth := a.layout.DashboardWidth()
	if x < leftGutter+dashWidth {
		return messages.PaneDashboard, true
	}

	// Keep hit-testing geometry in lockstep with app_view.go layout math:
	// dashboard, optional center (after gap), optional sidebar (after gap).
	centerStart := leftGutter + dashWidth
	if a.layout.ShowCenter() {
		centerStart += a.layout.GapX()
		centerEnd := centerStart + a.layout.CenterWidth()
		if x >= centerStart && x < centerEnd {
			return messages.PaneCenter, true
		}
		centerStart = centerEnd
	}

	if !a.layout.ShowSidebar() {
		return messages.PaneCenter, false
	}
	sidebarStart := centerStart + a.layout.GapX()
	sidebarEnd := sidebarStart + a.layout.SidebarWidth()
	if x < sidebarStart || x >= sidebarEnd {
		return messages.PaneCenter, false
	}

	localY := y - topGutter
	topPaneHeight, _ := sidebarPaneHeights(height)
	if localY >= topPaneHeight {
		return messages.PaneSidebarTerminal, true
	}
	return messages.PaneSidebar, true
}
