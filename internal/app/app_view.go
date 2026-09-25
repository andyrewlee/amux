package app

import (
	"fmt"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

const (
	fallbackWindowTitle = "amux"
)

// View renders the application using layer-based composition.
// This uses lipgloss Canvas to compose layers directly, enabling ultraviolet's
// cell-level differential rendering for optimal performance.
func (a *App) View() (view tea.View) {
	defer func() {
		if r := recover(); r != nil {
			wsID := "none"
			if a.activeWorkspace != nil {
				wsID = string(a.activeWorkspace.ID())
			}
			logging.Error("panic in app.View (workspace=%s, pane=%d): %v\n%s", wsID, a.focusedPane, r, debug.Stack())
			a.err = fmt.Errorf("render error: %v", r)
			// A half-built frame must not leave the last good frame reachable:
			// the error view would otherwise be replaced by stale content on the
			// next unchanged-key View.
			a.renderCache.frame.invalidate()
			view = a.fallbackView()
		}
	}()
	return a.view()
}

func (a *App) view() tea.View {
	defer perf.Time("view")()
	frameVersion := a.visibleFrameVersion()
	if cached, ok := a.renderCache.frame.get(frameVersion); ok {
		perf.Count("full_frame_cache_hit", 1)
		return a.finalizeView(cached)
	}
	perf.Count("full_frame_cache_miss", 1)

	baseView := func() tea.View {
		var view tea.View
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		view.BackgroundColor = common.ColorBackground()
		view.ForegroundColor = common.ColorForeground()
		view.KeyboardEnhancements.ReportEventTypes = true
		view.WindowTitle = fallbackWindowTitle
		return view
	}

	if a.quitting {
		view := baseView()
		view.SetContent("Goodbye!\n")
		return a.storeFrame(frameVersion, view)
	}

	if !a.ready {
		view := baseView()
		view.SetContent("Loading...")
		return a.storeFrame(frameVersion, view)
	}

	// Use layer-based rendering
	return a.storeFrame(frameVersion, a.viewLayerBased())
}

func (a *App) visibleFrameVersion() visibleFrameVersion {
	var version visibleFrameVersion
	if a.center != nil && a.layout != nil && a.layout.ShowCenter() {
		version.centerTerminal, version.centerTerminalTitle = a.center.VisibleTerminalVersions()
		version.centerActivity = a.center.ActivityVersion()
	}
	if a.sidebarTerminal != nil && a.layout != nil && a.layout.ShowSidebar() {
		version.sidebarTerminal = a.sidebarTerminal.VisibleTerminalVersion()
	}
	return version
}

func (a *App) storeFrame(version visibleFrameVersion, view tea.View) tea.View {
	a.renderCache.frame.store(version, view)
	return a.finalizeView(view)
}

func (a *App) canvasFor(width, height int) *lipgloss.Canvas {
	if width <= 0 || height <= 0 {
		width = 1
		height = 1
	}
	if a.canvas == nil {
		a.canvas = lipgloss.NewCanvas(width, height)
	} else if a.canvas.Width() != width || a.canvas.Height() != height {
		a.canvas.Resize(width, height)
	}
	a.canvas.Clear()
	return a.canvas
}

func (a *App) fallbackView() tea.View {
	view := tea.View{
		AltScreen:       true,
		BackgroundColor: common.ColorBackground(),
		ForegroundColor: common.ColorForeground(),
		WindowTitle:     fallbackWindowTitle,
	}
	msg := "A rendering error occurred."
	if a.err != nil {
		msg = "Error: " + a.err.Error()
	}
	view.SetContent(msg + "\n\nPress any key to dismiss.")
	return view
}

// viewLayerBased renders the application using lipgloss Canvas composition.
// This enables ultraviolet to perform cell-level differential updates.
func (a *App) viewLayerBased() tea.View {
	view := tea.View{
		AltScreen:            true,
		MouseMode:            tea.MouseModeCellMotion,
		BackgroundColor:      common.ColorBackground(),
		ForegroundColor:      common.ColorForeground(),
		KeyboardEnhancements: tea.KeyboardEnhancements{ReportEventTypes: true},
		WindowTitle:          fallbackWindowTitle,
	}
	if a.center != nil {
		view.WindowTitle = focusedWindowTitle(a.center.FocusedAgentTitle())
	}
	var terminalCursor *tea.Cursor
	setTerminalCursor := func(x, y int) {
		if x < 0 || y < 0 || x >= a.width || y >= a.height {
			return
		}
		cursor := tea.NewCursor(x, y)
		cursor.Blink = false
		terminalCursor = cursor
	}
	blockingOverlayVisible := a.overlayVisible()

	// Create canvas at screen dimensions
	canvas := a.canvasFor(a.width, a.height)

	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()

	a.composeDashboardPane(canvas, leftGutter, topGutter)
	if a.layout.ShowCenter() {
		a.composeCenterPane(canvas, leftGutter, topGutter, dashWidth, blockingOverlayVisible, setTerminalCursor)
	}
	if a.layout.ShowSidebar() {
		a.composeSidebarPane(canvas, leftGutter, topGutter, blockingOverlayVisible, setTerminalCursor)
	}

	// Overlay layers (dialogs, toasts, etc.)
	a.composeOverlays(canvas)

	cursor := a.overlayCursor()
	if cursor != nil && a.toastCoversPoint(cursor.X, cursor.Y) {
		cursor = nil
	}
	if cursor == nil &&
		!blockingOverlayVisible &&
		(a.focusedPane == messages.PaneCenter || a.focusedPane == messages.PaneSidebarTerminal) &&
		terminalCursor != nil &&
		!a.toastCoversPoint(terminalCursor.X, terminalCursor.Y) {
		cursor = terminalCursor
	}
	// No manual DEC-2026 wrap: bubbletea v2.0.9's renderer brackets each
	// update with its own synchronized-output markers when the terminal
	// reports mode-2026 support (cursed_renderer.go). Baking markers into
	// the content nested inside that region — the inner ?2026l could close
	// the renderer's atomic frame early.
	view.SetContent(canvas.Render())
	view.Cursor = cursor
	return view
}

const maxWindowTitleRunes = 128

func focusedWindowTitle(title string) string {
	if sanitized := sanitizedWindowTitle(title); sanitized != "" {
		return sanitized
	}
	return fallbackWindowTitle
}

func sanitizedWindowTitle(title string) string {
	return common.SanitizeDisplayText(title, maxWindowTitleRunes)
}
