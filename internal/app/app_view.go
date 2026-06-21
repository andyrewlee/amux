package app

import (
	"fmt"
	"runtime/debug"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// View renders the application using layer-based composition.
// This uses lipgloss Canvas to compose layers directly, enabling ultraviolet's
// cell-level differential rendering for optimal performance.
func (a *App) View() (view tea.View) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in app.View: %v\n%s", r, debug.Stack())
			a.err = fmt.Errorf("render error: %v", r)
			view = a.fallbackView()
		}
	}()
	return a.view()
}

func (a *App) view() tea.View {
	defer perf.Time("view")()

	baseView := func() tea.View {
		var view tea.View
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		view.BackgroundColor = common.ColorBackground()
		view.ForegroundColor = common.ColorForeground()
		view.KeyboardEnhancements.ReportEventTypes = true
		return view
	}

	if a.quitting {
		view := baseView()
		view.SetContent("Goodbye!\n")
		return a.finalizeView(view)
	}

	if !a.ready {
		view := baseView()
		view.SetContent("Loading...")
		return a.finalizeView(view)
	}

	// Use layer-based rendering
	return a.finalizeView(a.viewLayerBased())
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
	}
	if a.center != nil {
		if title := sanitizedWindowTitle(a.center.FocusedAgentTitle()); title != "" {
			view.WindowTitle = title
		}
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
	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = cursor
	return view
}

const maxWindowTitleRunes = 128

func sanitizedWindowTitle(title string) string {
	if title == "" {
		return ""
	}
	var b strings.Builder
	written := 0
	for len(title) > 0 && written < maxWindowTitleRunes {
		r, size := utf8.DecodeRuneInString(title)
		if r == utf8.RuneError && size == 1 {
			raw := title[0]
			title = title[1:]
			if isTerminalControlByte(raw) {
				continue
			}
		} else {
			title = title[size:]
		}
		if isTerminalControlRune(r) {
			continue
		}
		if b.Len() == 0 {
			b.Grow(len(title))
		}
		b.WriteRune(r)
		written++
	}
	return b.String()
}

func isTerminalControlByte(b byte) bool {
	return b <= 0x1f || b == 0x7f || (b >= 0x80 && b <= 0x9f)
}

func isTerminalControlRune(r rune) bool {
	return r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
