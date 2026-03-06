package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

const paneNone messages.PaneType = -1

// routeMouseClick routes mouse click events to the appropriate pane.
func (a *App) routeMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if a.prefixPaletteContainsPoint(msg.X, msg.Y) {
		// Palette clicks are currently non-interactive; consume to prevent
		// accidental clicks in underlying panes while prefix mode is active.
		return nil
	}

	targetPane, hasTarget := a.paneForPoint(msg.X, msg.Y)

	// Left-click updates keyboard focus; other buttons preserve keyboard focus.
	var focusCmd tea.Cmd
	if msg.Button == tea.MouseLeft && hasTarget {
		focusCmd = a.focusPane(targetPane)
	}

	if cmd := a.handleCenterPaneClick(msg); cmd != nil {
		return common.SafeBatch(focusCmd, cmd)
	}

	// Intentional pointer-target routing (not focused-pane routing): clicks go to
	// the pane under the pointer, including right/middle buttons.
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
	// Route wheel input by keyboard focus; child models currently ignore wheel
	// while unfocused.
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
		return paneNone, false
	}
	topGutter := a.layout.TopGutter()
	height := a.layout.Height()
	if y < topGutter || y >= topGutter+height {
		return paneNone, false
	}

	leftGutter := a.layout.LeftGutter()
	if x < leftGutter {
		// Outer gutter is intentionally non-interactive; do not retarget focus.
		return paneNone, false
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
			// Center column: top = agent, bottom = terminal
			localY := y - topGutter
			centerTopHeight, _ := centerPaneHeights(height)
			if localY >= centerTopHeight {
				return messages.PaneSidebarTerminal, true
			}
			return messages.PaneCenter, true
		}
		centerStart = centerEnd
	}

	if !a.layout.ShowSidebar() {
		return paneNone, false
	}
	sidebarStart := centerStart + a.layout.GapX()
	sidebarEnd := sidebarStart + a.layout.SidebarWidth()
	// Inter-pane gaps are intentionally non-interactive.
	if x < sidebarStart || x >= sidebarEnd {
		return paneNone, false
	}

	return messages.PaneSidebar, true
}

func (a *App) prefixPaletteContainsPoint(x, y int) bool {
	if !a.prefixActive || a.width <= 0 || a.height <= 0 {
		return false
	}
	palette := a.renderPrefixPalette()
	if palette == "" {
		return false
	}
	_, paletteHeight := viewDimensions(palette)
	if paletteHeight <= 0 {
		return false
	}
	paletteY := a.height - paletteHeight
	if paletteY < 0 {
		paletteY = 0
	}
	return x >= 0 && x < a.width && y >= paletteY && y < a.height
}
