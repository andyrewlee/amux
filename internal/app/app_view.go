package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// Synchronized Output Mode 2026 sequences
// https://gist.github.com/christianparpart/d8a62cc1ab659194337d73e399004036
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// View renders the application using layer-based composition.
// This uses lipgloss Canvas to compose layers directly, enabling ultraviolet's
// cell-level differential rendering for optimal performance.
func (a *App) View() tea.View {
	defer perf.Time("view")()

	baseView := func() tea.View {
		var view tea.View
		view.AltScreen = true
		view.MouseMode = tea.MouseModeCellMotion
		view.BackgroundColor = common.ColorBackground
		view.ForegroundColor = common.ColorForeground
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

	// Monitor mode uses the compositor for a full-screen grid.
	if a.monitorMode {
		return a.finalizeView(a.viewMonitorMode())
	}

	// Use layer-based rendering for normal mode
	return a.finalizeView(a.viewLayerBased())
}

// viewLayerBased renders the application using lipgloss Canvas composition.
// This enables ultraviolet to perform cell-level differential updates.
func (a *App) viewLayerBased() tea.View {
	view := tea.View{
		AltScreen:            true,
		MouseMode:            tea.MouseModeCellMotion,
		BackgroundColor:      common.ColorBackground,
		ForegroundColor:      common.ColorForeground,
		KeyboardEnhancements: tea.KeyboardEnhancements{ReportEventTypes: true},
	}

	// Create canvas at screen dimensions
	canvas := a.canvasFor(a.width, a.height)

	// Dashboard pane (leftmost)
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()
	dashHeight := a.layout.Height()
	dashFocused := a.dashboard.Focused()
	dashContentWidth := dashWidth - 4
	dashContentHeight := dashHeight - 2
	if dashContentWidth < 1 {
		dashContentWidth = 1
	}
	if dashContentHeight < 1 {
		dashContentHeight = 1
	}
	dashContent := clampLines(a.dashboard.View(), dashContentWidth, dashContentHeight)
	if dashDrawable := a.dashboardContent.get(dashContent, leftGutter+2, topGutter+1); dashDrawable != nil {
		canvas.Compose(dashDrawable)
	}
	for _, border := range a.dashboardBorders.get(leftGutter, topGutter, dashWidth, dashHeight, dashFocused) {
		canvas.Compose(border)
	}

	// Center pane
	if a.layout.ShowCenter() {
		centerX := leftGutter + dashWidth + a.layout.GapX()
		centerWidth := a.layout.CenterWidth()
		centerHeight := a.layout.Height()
		centerFocused := a.focusedPane == messages.PaneCenter

		// Check if we can use VTermLayer for direct cell rendering
		if termLayer := a.center.TerminalLayer(); termLayer != nil && a.center.HasTabs() && !a.center.HasCommitViewer() {
			// Get terminal viewport from center model (accounts for borders, tab bar, help lines)
			termOffsetX, termOffsetY, termW, termH := a.center.TerminalViewport()
			termX := centerX + termOffsetX
			termY := topGutter + termOffsetY

			// Compose terminal layer first; chrome is drawn on top without clearing the content area.
			positionedTermLayer := &compositor.PositionedVTermLayer{
				VTermLayer: termLayer,
				PosX:       termX,
				PosY:       termY,
				Width:      termW,
				Height:     termH,
			}
			canvas.Compose(positionedTermLayer)

			// Draw borders without touching the content area.
			for _, border := range a.centerBorders.get(centerX, topGutter, centerWidth, centerHeight, centerFocused) {
				canvas.Compose(border)
			}

			contentWidth := a.center.ContentWidth()
			if contentWidth < 1 {
				contentWidth = 1
			}

			// Tab bar (top of content area).
			tabBar := clampLines(a.center.TabBarView(), contentWidth, termOffsetY-1)
			if tabBarDrawable := a.centerTabBar.get(tabBar, termX, topGutter+1); tabBarDrawable != nil {
				canvas.Compose(tabBarDrawable)
			}

			// Status line (directly below terminal content).
			if status := clampLines(a.center.ActiveTerminalStatusLine(), contentWidth, 1); status != "" {
				if statusDrawable := a.centerStatus.get(status, termX, termY+termH); statusDrawable != nil {
					canvas.Compose(statusDrawable)
				}
			}

			// Help lines at bottom of pane.
			if helpLines := a.center.HelpLines(contentWidth); len(helpLines) > 0 {
				helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
				helpY := topGutter + centerHeight - 1 - len(helpLines)
				if helpY > termY {
					if helpDrawable := a.centerHelp.get(helpContent, termX, helpY); helpDrawable != nil {
						canvas.Compose(helpDrawable)
					}
				}
			}
		} else {
			// Fallback to string-based rendering with borders (no caching - content changes)
			a.centerChrome.Invalidate()
			var centerContent string
			if a.center.HasTabs() {
				centerContent = a.center.View()
			} else {
				centerContent = a.renderCenterPaneContent()
			}
			centerView := buildBorderedPane(centerContent, centerWidth, centerHeight, centerFocused)
			centerDrawable := compositor.NewStringDrawable(clampPane(centerView, centerWidth, centerHeight), centerX, topGutter)
			canvas.Compose(centerDrawable)
		}
	}

	// Sidebar pane (rightmost)
	if a.layout.ShowSidebar() {
		sidebarX := leftGutter + a.layout.DashboardWidth()
		if a.layout.ShowCenter() {
			sidebarX += a.layout.GapX() + a.layout.CenterWidth()
		}
		if a.layout.ShowSidebar() {
			sidebarX += a.layout.GapX()
		}
		sidebarWidth := a.layout.SidebarWidth()
		sidebarHeight := a.layout.Height()
		topPaneHeight, bottomPaneHeight := sidebarPaneHeights(sidebarHeight)
		if bottomPaneHeight > 0 {
			contentWidth := sidebarWidth - 4
			if contentWidth < 1 {
				contentWidth = 1
			}

			topFocused := a.focusedPane == messages.PaneSidebar
			bottomFocused := a.focusedPane == messages.PaneSidebarTerminal

			if topPaneHeight > 0 {
				topContentHeight := topPaneHeight - 2
				if topContentHeight < 1 {
					topContentHeight = 1
				}
				topContent := clampLines(a.sidebar.View(), contentWidth, topContentHeight)
				if topDrawable := a.sidebarTopContent.get(topContent, sidebarX+2, topGutter+1); topDrawable != nil {
					canvas.Compose(topDrawable)
				}
				for _, border := range a.sidebarTopBorders.get(sidebarX, topGutter, sidebarWidth, topPaneHeight, topFocused) {
					canvas.Compose(border)
				}
			}

			bottomY := topGutter + topPaneHeight
			bottomContentHeight := bottomPaneHeight - 2
			if bottomContentHeight < 1 {
				bottomContentHeight = 1
			}

			if termLayer := a.sidebarTerminal.TerminalLayer(); termLayer != nil {
				originX, originY := a.sidebarTerminal.TerminalOrigin()
				termW, termH := a.sidebarTerminal.TerminalSize()
				if termW > contentWidth {
					termW = contentWidth
				}
				if termH > bottomContentHeight {
					termH = bottomContentHeight
				}

				status := clampLines(a.sidebarTerminal.StatusLine(), contentWidth, 1)
				helpLines := a.sidebarTerminal.HelpLines(contentWidth)
				statusLines := 0
				if status != "" {
					statusLines = 1
				}
				maxHelpHeight := bottomContentHeight - statusLines
				if maxHelpHeight < 0 {
					maxHelpHeight = 0
				}
				if len(helpLines) > maxHelpHeight {
					helpLines = helpLines[:maxHelpHeight]
				}
				maxTermHeight := bottomContentHeight - statusLines - len(helpLines)
				if maxTermHeight < 0 {
					maxTermHeight = 0
				}
				if termH > maxTermHeight {
					termH = maxTermHeight
				}

				positioned := &compositor.PositionedVTermLayer{
					VTermLayer: termLayer,
					PosX:       originX,
					PosY:       originY,
					Width:      termW,
					Height:     termH,
				}
				canvas.Compose(positioned)

				if status != "" {
					if statusDrawable := a.sidebarBottomStatus.get(status, originX, originY+termH); statusDrawable != nil {
						canvas.Compose(statusDrawable)
					}
				}

				if len(helpLines) > 0 {
					helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
					helpY := originY + bottomContentHeight - len(helpLines)
					if helpDrawable := a.sidebarBottomHelp.get(helpContent, originX, helpY); helpDrawable != nil {
						canvas.Compose(helpDrawable)
					}
				} else if status == "" && bottomContentHeight > termH {
					blank := strings.Repeat(" ", contentWidth)
					if blankDrawable := a.sidebarBottomHelp.get(blank, originX, originY+bottomContentHeight-1); blankDrawable != nil {
						canvas.Compose(blankDrawable)
					}
				}
			} else {
				bottomContent := clampLines(a.sidebarTerminal.View(), contentWidth, bottomContentHeight)
				if bottomDrawable := a.sidebarBottomContent.get(bottomContent, sidebarX+2, bottomY+1); bottomDrawable != nil {
					canvas.Compose(bottomDrawable)
				}
			}
			for _, border := range a.sidebarBottomBorders.get(sidebarX, bottomY, sidebarWidth, bottomPaneHeight, bottomFocused) {
				canvas.Compose(border)
			}
		}
	}

	// Overlay layers (dialogs, toasts, etc.)
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}

// composeOverlays adds overlay layers (dialogs, toasts, help, etc.) to the canvas.
func (a *App) composeOverlays(canvas *lipgloss.Canvas) {
	// Dialog overlay
	if a.dialog != nil && a.dialog.Visible() {
		dialogView := a.dialog.View()
		dialogWidth, dialogHeight := viewDimensions(dialogView)
		x, y := a.centeredPosition(dialogWidth, dialogHeight)
		dialogDrawable := compositor.NewStringDrawable(dialogView, x, y)
		canvas.Compose(dialogDrawable)
	}

	// File picker overlay
	if a.filePicker != nil && a.filePicker.Visible() {
		pickerView := a.filePicker.View()
		pickerWidth, pickerHeight := viewDimensions(pickerView)
		x, y := a.centeredPosition(pickerWidth, pickerHeight)
		pickerDrawable := compositor.NewStringDrawable(pickerView, x, y)
		canvas.Compose(pickerDrawable)
	}

	// Settings dialog overlay
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		settingsView := a.settingsDialog.View()
		settingsWidth, settingsHeight := viewDimensions(settingsView)
		x, y := a.centeredPosition(settingsWidth, settingsHeight)
		settingsDrawable := compositor.NewStringDrawable(settingsView, x, y)
		canvas.Compose(settingsDrawable)
	}

	// Help overlay (centered like settings dialog)
	if a.helpOverlay.Visible() {
		helpView := a.helpOverlay.View()
		helpWidth, helpHeight := viewDimensions(helpView)
		x, y := a.centeredPosition(helpWidth, helpHeight)
		helpDrawable := compositor.NewStringDrawable(helpView, x, y)
		canvas.Compose(helpDrawable)
	}

	// Prefix mode indicator
	if a.prefixActive {
		indicator := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#7aa2f7")).
			Padding(0, 1).
			Render("PREFIX")
		indicatorWidth := lipgloss.Width(indicator)
		x := a.width - indicatorWidth - 1
		y := a.height - 3
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		prefixDrawable := compositor.NewStringDrawable(indicator, x, y)
		canvas.Compose(prefixDrawable)
	}

	// Toast notification
	if a.toast.Visible() {
		toastView := a.toast.View()
		if toastView != "" {
			toastWidth := lipgloss.Width(toastView)
			x := (a.width - toastWidth) / 2
			y := a.height - 2
			if x < 0 {
				x = 0
			}
			if y < 0 {
				y = 0
			}
			toastDrawable := compositor.NewStringDrawable(toastView, x, y)
			canvas.Compose(toastDrawable)
		}
	}

	// Error overlay
	if a.err != nil {
		errView := a.renderErrorOverlay()
		errWidth, errHeight := viewDimensions(errView)
		x, y := a.centeredPosition(errWidth, errHeight)
		errDrawable := compositor.NewStringDrawable(errView, x, y)
		canvas.Compose(errDrawable)
	}
}

// renderErrorOverlay returns the error overlay content.
func (a *App) renderErrorOverlay() string {
	if a.err == nil {
		return ""
	}
	errStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(1, 2).
		MaxWidth(60)
	return errStyle.Render("Error: " + a.err.Error() + "\n\nPress any key to dismiss.")
}

func (a *App) finalizeView(view tea.View) tea.View {
	if a.pendingInputLatency {
		perf.Record("input_latency", time.Since(a.lastInputAt))
		a.pendingInputLatency = false
	}
	return view
}

func clampPane(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(view)
}

func clampLines(content string, width, maxLines int) string {
	if content == "" || width <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

func viewDimensions(view string) (width, height int) {
	lines := strings.Split(view, "\n")
	height = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width, height
}

func (a *App) centeredPosition(width, height int) (x, y int) {
	x = (a.width - width) / 2
	y = (a.height - height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

func (a *App) adjustSidebarMouseY(y int) int {
	if a.layout == nil {
		return y
	}
	// Sidebar content starts one row below the top border.
	return y - a.layout.TopGutter() - 1
}

func (a *App) overlayCursor() *tea.Cursor {
	if a.dialog != nil && a.dialog.Visible() {
		if c := a.dialog.Cursor(); c != nil {
			dialogView := a.dialog.View()
			dialogWidth, dialogHeight := viewDimensions(dialogView)
			x, y := a.centeredPosition(dialogWidth, dialogHeight)
			c.X += x
			c.Y += y
			return c
		}
		return nil
	}

	if a.filePicker != nil && a.filePicker.Visible() {
		if c := a.filePicker.Cursor(); c != nil {
			pickerView := a.filePicker.View()
			pickerWidth, pickerHeight := viewDimensions(pickerView)
			x, y := a.centeredPosition(pickerWidth, pickerHeight)
			c.X += x
			c.Y += y
			return c
		}
	}

	return nil
}
