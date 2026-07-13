package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

const paneNone messages.PaneType = -1

// dispatchToPane forwards a mouse message to the child component for the given
// pane: it translates the pointer coordinates into the pane's local space (the
// sidebar terminal needs no adjustment and is forwarded unchanged), calls the
// child's Update, stores the returned model back, and returns the child's Cmd.
// It returns nil for an unrecognized pane. Callers layer any focus/SafeBatch
// behavior on top of the returned Cmd, so this helper performs no focus
// bookkeeping itself. The concrete message type is preserved through the
// coordinate adjust so each child's own type switch still matches.
func (a *App) dispatchToPane(pane messages.PaneType, msg tea.Msg) tea.Cmd {
	switch pane {
	case messages.PaneDashboard:
		newDashboard, cmd := a.dashboard.Update(a.adjustMouseMsg(pane, msg))
		a.dashboard = newDashboard
		return cmd
	case messages.PaneCenter:
		newCenter, cmd := a.center.Update(a.adjustMouseMsg(pane, msg))
		a.center = newCenter
		return cmd
	case messages.PaneSidebar:
		newSidebar, cmd := a.sidebar.Update(a.adjustMouseMsg(pane, msg))
		a.sidebar = newSidebar
		return cmd
	case messages.PaneSidebarTerminal:
		// The sidebar terminal renders in screen space, so its message is
		// forwarded without coordinate translation.
		newTerm, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerm
		return cmd
	}
	return nil
}

// adjustMouseMsg returns a copy of the mouse message with its X/Y translated
// into the target pane's local coordinate space via adjustMouseXY, preserving
// the concrete message type. Non-mouse messages are returned unchanged.
func (a *App) adjustMouseMsg(pane messages.PaneType, msg tea.Msg) tea.Msg {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		m.X, m.Y = a.adjustMouseXY(pane, m.X, m.Y)
		return m
	case tea.MouseWheelMsg:
		m.X, m.Y = a.adjustMouseXY(pane, m.X, m.Y)
		return m
	case tea.MouseMotionMsg:
		m.X, m.Y = a.adjustMouseXY(pane, m.X, m.Y)
		return m
	case tea.MouseReleaseMsg:
		m.X, m.Y = a.adjustMouseXY(pane, m.X, m.Y)
		return m
	}
	return msg
}

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

	cmd := a.dispatchToPane(targetPane, msg)
	if targetPane == messages.PaneSidebarTerminal {
		// If the click returned a command (e.g., CreateNewTab from "+ New" button),
		// skip focusCmd to avoid double terminal creation.
		if cmd != nil {
			return cmd
		}
		return focusCmd
	}
	return common.SafeBatch(focusCmd, cmd)
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

// adjustMouseXY converts pointer coordinates into the target pane's local
// coordinate space. It returns the input unchanged when layout is unset or the
// pane needs no adjustment (e.g. the sidebar terminal).
func (a *App) adjustMouseXY(pane messages.PaneType, x, y int) (int, int) {
	if a.layout == nil {
		return x, y
	}
	switch pane {
	case messages.PaneDashboard:
		return x - a.layout.LeftGutter(), y - a.layout.TopGutter()
	case messages.PaneCenter:
		return x, y - a.layout.TopGutter()
	case messages.PaneSidebar:
		return a.adjustSidebarMouseXY(x, y)
	default:
		return x, y
	}
}

// routeMouseWheel routes mouse wheel events to the appropriate pane.
func (a *App) routeMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if a.prefixPaletteContainsPoint(msg.X, msg.Y) {
		// Palette wheel input is currently non-interactive; consume it so hidden
		// panes cannot scroll or steal focus while prefix mode is active.
		return nil
	}

	if (a.dialog != nil && a.dialog.Visible()) ||
		(a.filePicker != nil && a.filePicker.Visible()) ||
		(a.settingsDialog != nil && a.settingsDialog.Visible()) ||
		(a.envDialog != nil && a.envDialog.Visible()) ||
		a.err != nil ||
		a.toastCoversPoint(msg.X, msg.Y) {
		// Modal, error, and toast overlays should block background scrolling.
		return nil
	}

	targetPane := a.focusedPane
	// Route wheel input by pointer target when possible so hovered panes scroll
	// without requiring a prior click. If the pointer is over another pane that
	// cannot handle wheel input, consume the event instead of scrolling the
	// previously focused pane behind it.
	hoverPane, hasTarget := a.paneForPoint(msg.X, msg.Y)
	if hasTarget && hoverPane != a.focusedPane {
		// Dashboard wheel handling activates rows, so do not retarget passive
		// hover wheel input into it from another pane.
		if hoverPane == messages.PaneDashboard {
			return nil
		}
		if !a.canRetargetWheelToPane(hoverPane) {
			return nil
		}
		targetPane = hoverPane
	}

	var focusCmd tea.Cmd
	if targetPane != a.focusedPane {
		focusCmd = a.focusPaneOnWheel(targetPane)
	}

	switch targetPane {
	case messages.PaneDashboard, messages.PaneCenter, messages.PaneSidebar, messages.PaneSidebarTerminal:
		return common.SafeBatch(focusCmd, a.dispatchToPane(targetPane, msg))
	}
	return nil
}

func (a *App) canRetargetWheelToPane(pane messages.PaneType) bool {
	switch pane {
	case messages.PaneCenter:
		return a.center != nil && a.center.CanConsumeWheel()
	case messages.PaneSidebar:
		return a.sidebar != nil && a.sidebar.CanConsumeWheel()
	case messages.PaneSidebarTerminal:
		return a.sidebarTerminal != nil && a.sidebarTerminal.CanConsumeWheel()
	default:
		return false
	}
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
	case messages.PaneDashboard, messages.PaneCenter, messages.PaneSidebar, messages.PaneSidebarTerminal:
		return a.dispatchToPane(targetPane, msg)
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
	case messages.PaneDashboard, messages.PaneCenter, messages.PaneSidebar, messages.PaneSidebarTerminal:
		return a.dispatchToPane(targetPane, msg)
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

	localY := y - topGutter
	topPaneHeight, _ := sidebarPaneHeights(height)
	if localY >= topPaneHeight {
		return messages.PaneSidebarTerminal, true
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
