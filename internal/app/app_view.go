package app

import (
	"fmt"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/perf"
	"github.com/andyrewlee/medusa/internal/ui/common"
	"github.com/andyrewlee/medusa/internal/ui/compositor"
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

func (a *App) fallbackView() tea.View {
	view := tea.View{
		AltScreen:       true,
		BackgroundColor: common.ColorBackground,
		ForegroundColor: common.ColorForeground,
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
	dashFocused := a.focusedPane == messages.PaneDashboard
	dashContentWidth := dashWidth - 3
	dashContentHeight := dashHeight - 2
	if dashContentWidth < 1 {
		dashContentWidth = 1
	}
	if dashContentHeight < 1 {
		dashContentHeight = 1
	}
	dashContent := clampLines(a.dashboard.View(), dashContentWidth, dashContentHeight)
	if dashDrawable := a.dashboardContent.get(dashContent, leftGutter+1, topGutter+1); dashDrawable != nil {
		canvas.Compose(dashDrawable)
	}
	for _, border := range a.dashboardBorders.get(leftGutter, topGutter, dashWidth, dashHeight, dashFocused) {
		canvas.Compose(border)
	}

	// Dashboard scrollbar
	offset, total, visible := a.dashboard.ScrollInfo()
	if total > visible {
		trackLength := dashHeight - 2
		if sb := scrollbarDrawable(leftGutter+dashWidth-1, topGutter+1, trackLength, offset, total, visible); sb != nil {
			canvas.Compose(sb)
		}
	}

	// Center pane
	centerX := leftGutter + dashWidth + a.layout.GapX()
	// Update info content for the Info tab
	if a.activeWorkspace != nil {
		a.center.SetInfoContent(a.renderWorkspaceInfo())
	}
	if a.layout.ShowCenter() {
		centerWidth := a.layout.CenterWidth()
		centerHeight := a.layout.CenterContentHeight() // Height minus terminal
		centerFocused := a.focusedPane == messages.PaneCenter

		// Check if we can use VTermLayer for direct cell rendering
		if termLayer := a.center.TerminalLayer(); termLayer != nil && a.center.HasTabs() && !a.center.HasDiffViewer() {
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

			// Info bar at the very top (line 0 inside border).
			infoBarHeight := a.center.InfoBarHeight()
			infoBarY := topGutter + 1
			if infoBarHeight > 0 {
				infoBarContent := clampLines(a.center.InfoBarView(contentWidth), contentWidth, infoBarHeight)
				if infoBarContent != "" {
					if infoBarDrawable := a.centerActionBar.get(infoBarContent, termX, infoBarY); infoBarDrawable != nil {
						canvas.Compose(infoBarDrawable)
					}
					// Set content-relative Y for mouse hit testing (line 0 = top of content)
					a.center.SetInfoBarY(0)
				}
			}

			// Tab bar (below info bar) — includes separator line.
			tabBarY := infoBarY + infoBarHeight
			tabBar := clampLines(a.center.TabBarView(), contentWidth, 2)
			if tabBarDrawable := a.centerTabBar.get(tabBar, termX, tabBarY); tabBarDrawable != nil {
				canvas.Compose(tabBarDrawable)
			}

			// Status line (directly below terminal content).
			if status := clampLines(a.center.ActiveTerminalStatusLine(), contentWidth, 1); status != "" {
				if statusDrawable := a.centerStatus.get(status, termX, termY+termH); statusDrawable != nil {
					canvas.Compose(statusDrawable)
				}
			}

			// Help lines at bottom of pane.
			helpLines := a.center.HelpLines(contentWidth)
			helpLineCount := len(helpLines)
			if helpLineCount > 0 {
				helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, helpLineCount)
				helpY := topGutter + centerHeight - 1 - helpLineCount
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
			if a.center.HasTabs() || a.center.IsInfoTabActive() {
				centerContent = a.center.View()
			} else {
				centerContent = a.renderCenterPaneContent()
			}
			centerView := buildBorderedPane(centerContent, centerWidth, centerHeight, centerFocused)
			centerDrawable := compositor.NewStringDrawable(clampPane(centerView, centerWidth, centerHeight), centerX, topGutter)
			canvas.Compose(centerDrawable)
		}
	}

	// Terminal pane (below center pane)
	if a.layout.ShowTerminal() {
		terminalX := centerX
		terminalY := topGutter + a.layout.CenterContentHeight()
		terminalWidth := a.layout.CenterWidth()
		terminalHeight := a.layout.TerminalHeight()
		terminalFocused := a.focusedPane == messages.PaneTerminal
		terminalCollapsed := a.layout.TerminalCollapsed()

		contentWidth := terminalWidth - 4 // border + padding
		if contentWidth < 1 {
			contentWidth = 1
		}
		contentHeight := terminalHeight - 2
		if contentHeight < 1 {
			contentHeight = 1
		}

		// Toggle button position (top right, inside border)
		toggleIcon := "▼" // expanded
		if terminalCollapsed {
			toggleIcon = "▲" // collapsed
		}
		toggleX := terminalX + terminalWidth - 3 // -1 for border, -2 for padding and icon
		toggleY := terminalY + 1                 // inside top border
		a.terminalToggleX = toggleX
		a.terminalToggleY = toggleY

		// Header content - different for collapsed vs expanded
		headerY := terminalY + 1 // Inside the border
		originX := terminalX + 2 // border + padding

		if terminalCollapsed {
			// When collapsed, show simple label with hint
			labelStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
			label := labelStyle.Render("Terminal (click ▲ to expand)")
			labelWidth := contentWidth - 1 // leave space for toggle
			labelContent := clampLines(label, labelWidth, 1)
			if labelDrawable := a.terminalTabBar.get(labelContent, originX, headerY); labelDrawable != nil {
				canvas.Compose(labelDrawable)
			}
		} else {
			// When expanded, show tab bar
			tabBar := a.sidebarTerminal.TabBarView()
			tabBarWidth := contentWidth - 1 // leave space for toggle
			if tabBarWidth < 1 {
				tabBarWidth = 1
			}
			if tabBar != "" {
				tabBarContent := clampLines(tabBar, tabBarWidth, 1)
				if tabBarDrawable := a.terminalTabBar.get(tabBarContent, originX, headerY); tabBarDrawable != nil {
					canvas.Compose(tabBarDrawable)
				}
			}
		}

		// Always render toggle button
		toggleStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
		toggleContent := toggleStyle.Render(toggleIcon)
		toggleDrawable := compositor.NewStringDrawable(toggleContent, toggleX, toggleY)
		canvas.Compose(toggleDrawable)

		// Only render terminal content when expanded
		if !terminalCollapsed {
			if termLayer := a.sidebarTerminal.TerminalLayer(); termLayer != nil {
				_, originY := a.sidebarTerminal.TerminalOrigin()
				termW, termH := a.sidebarTerminal.TerminalSize()

				// Clamp VTerm dimensions to content area
				if termW > contentWidth {
					termW = contentWidth
				}
				if termH > contentHeight-1 { // -1 for tab bar
					termH = contentHeight - 1
				}

				status := clampLines(a.sidebarTerminal.StatusLine(), contentWidth, 1)
				helpLines := a.sidebarTerminal.HelpLines(contentWidth)
				statusLines := 0
				if status != "" {
					statusLines = 1
				}
				maxHelpHeight := contentHeight - statusLines - 1 // -1 for tab bar
				if maxHelpHeight < 0 {
					maxHelpHeight = 0
				}
				if len(helpLines) > maxHelpHeight {
					helpLines = helpLines[:maxHelpHeight]
				}
				maxTermHeight := contentHeight - statusLines - len(helpLines) - 1 // -1 for tab bar
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
					if statusDrawable := a.terminalStatus.get(status, originX, originY+termH); statusDrawable != nil {
						canvas.Compose(statusDrawable)
					}
				}

				if len(helpLines) > 0 {
					helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
					helpY := originY + contentHeight - len(helpLines) - 1 // -1 for tab bar
					if helpDrawable := a.terminalHelp.get(helpContent, originX, helpY); helpDrawable != nil {
						canvas.Compose(helpDrawable)
					}
				}
			} else {
				// Use ContentView() to avoid duplicating the tab bar (already rendered above)
				termContent := clampLines(a.sidebarTerminal.ContentView(), contentWidth, contentHeight-1) // -1 for tab bar
				if termContent != "" {
					if termDrawable := a.terminalHelp.get(termContent, terminalX+2, terminalY+2); termDrawable != nil {
						canvas.Compose(termDrawable)
					}
				}
			}
		}
		for _, border := range a.terminalBorders.get(terminalX, terminalY, terminalWidth, terminalHeight, terminalFocused) {
			canvas.Compose(border)
		}
	}

	// Sidebar pane (rightmost) - now a single full-height pane for git changes
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
		sidebarFocused := a.focusedPane == messages.PaneSidebar

		contentWidth := sidebarWidth - 4 // border + padding
		if contentWidth < 1 {
			contentWidth = 1
		}
		contentHeight := sidebarHeight - 2
		if contentHeight < 1 {
			contentHeight = 1
		}

		// Sidebar tab bar (Changes/Project tabs)
		tabBar := a.sidebar.TabBarView()
		tabBarHeight := 0
		if tabBar != "" {
			tabBarHeight = 1
			tabBarContent := clampLines(tabBar, contentWidth, 1)
			tabBarY := topGutter + 1 // Inside the border
			if tabBarDrawable := a.sidebarTabBar.get(tabBarContent, sidebarX+2, tabBarY); tabBarDrawable != nil {
				canvas.Compose(tabBarDrawable)
			}
		}

		// Sidebar content (below tab bar)
		sidebarContentHeight := contentHeight - tabBarHeight
		if sidebarContentHeight < 1 {
			sidebarContentHeight = 1
		}
		sidebarContent := clampLines(a.sidebar.ContentView(), contentWidth, sidebarContentHeight)
		if sidebarDrawable := a.sidebarContent.get(sidebarContent, sidebarX+2, topGutter+1+tabBarHeight); sidebarDrawable != nil {
			canvas.Compose(sidebarDrawable)
		}
		for _, border := range a.sidebarBorders.get(sidebarX, topGutter, sidebarWidth, sidebarHeight, sidebarFocused) {
			canvas.Compose(border)
		}
	}

	// Overlay layers (dialogs, toasts, etc.)
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}
