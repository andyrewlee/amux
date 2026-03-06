package app

import (
	"fmt"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/logging"
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

	// Create canvas at screen dimensions
	canvas := a.canvasFor(a.width, a.height)

	// Dashboard pane (leftmost) - full height
	leftGutter := a.layout.LeftGutter()
	topGutter := a.layout.TopGutter()
	dashWidth := a.layout.DashboardWidth()
	dashHeight := a.layout.Height()
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
	for _, border := range a.dashboardBorders.get(leftGutter, topGutter, dashWidth, dashHeight) {
		canvas.Compose(border)
	}

	// Center pane: agent (top 3/4) + terminal (bottom 1/4)
	if a.layout.ShowCenter() {
		centerX := leftGutter + dashWidth + a.layout.GapX()
		centerWidth := a.layout.CenterWidth()
		centerTopHeight, centerBottomHeight := centerPaneHeights(a.layout.Height())

		// Agent pane (top portion)
		centerOwnsCursor := a.focusedPane == messages.PaneCenter
		if termLayer := a.center.TerminalLayerWithCursorOwner(centerOwnsCursor); termLayer != nil && a.center.HasTabs() && !a.center.HasDiffViewer() {
			termOffsetX, termOffsetY, termW, termH := a.center.TerminalViewport()
			termX := centerX + termOffsetX
			termY := topGutter + termOffsetY
			canvas.Compose(&compositor.PositionedVTermLayer{
				VTermLayer: termLayer,
				PosX:       termX,
				PosY:       termY,
				Width:      termW,
				Height:     termH,
			})
			for _, border := range a.centerBorders.get(centerX, topGutter, centerWidth, centerTopHeight) {
				canvas.Compose(border)
			}
			contentWidth := a.center.ContentWidth()
			if contentWidth < 1 {
				contentWidth = 1
			}
			tabBar := clampLines(a.center.TabBarView(), contentWidth, termOffsetY-1)
			if tabBarDrawable := a.centerTabBar.get(tabBar, termX, topGutter+1); tabBarDrawable != nil {
				canvas.Compose(tabBarDrawable)
			}
			if status := clampLines(a.center.ActiveTerminalStatusLine(), contentWidth, 1); status != "" {
				if statusDrawable := a.centerStatus.get(status, termX, termY+termH); statusDrawable != nil {
					canvas.Compose(statusDrawable)
				}
			}
			if helpLines := a.center.HelpLines(contentWidth); len(helpLines) > 0 {
				helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
				helpY := topGutter + centerTopHeight - 1 - len(helpLines)
				if helpY > termY {
					if helpDrawable := a.centerHelp.get(helpContent, termX, helpY); helpDrawable != nil {
						canvas.Compose(helpDrawable)
					}
				}
			}
		} else {
			a.centerChrome.Invalidate()
			var centerContent string
			if a.center.HasTabs() {
				centerContent = a.center.View()
			} else {
				centerContent = a.renderCenterPaneContent()
			}
			centerView := buildBorderedPane(centerContent, centerWidth, centerTopHeight)
			centerDrawable := compositor.NewStringDrawable(clampPane(centerView, centerWidth, centerTopHeight), centerX, topGutter)
			canvas.Compose(centerDrawable)
		}

		// Terminal pane (bottom quarter of center column)
		if centerBottomHeight > 0 {
			termPaneY := topGutter + centerTopHeight
			termContentWidth := centerWidth - 4
			termContentHeight := centerBottomHeight - 2
			if termContentWidth < 1 {
				termContentWidth = 1
			}
			if termContentHeight < 1 {
				termContentHeight = 1
			}
			sidebarOwnsCursor := a.focusedPane == messages.PaneSidebarTerminal
			if termLayer := a.sidebarTerminal.TerminalLayerWithCursorOwner(sidebarOwnsCursor); termLayer != nil {
				originX, originY := a.sidebarTerminal.TerminalOrigin()
				termW, termH := a.sidebarTerminal.TerminalSize()
				if termW > termContentWidth {
					termW = termContentWidth
				}
				if termH > termContentHeight {
					termH = termContentHeight
				}
				tabBar := a.sidebarTerminal.TabBarView()
				tabBarHeight := 0
				if tabBar != "" {
					tabBarHeight = 1
					tabBarContent := clampLines(tabBar, termContentWidth, 1)
					if tabBarDrawable := a.sidebarBottomTabBar.get(tabBarContent, originX, termPaneY+1); tabBarDrawable != nil {
						canvas.Compose(tabBarDrawable)
					}
				}
				status := clampLines(a.sidebarTerminal.StatusLine(), termContentWidth, 1)
				helpLines := a.sidebarTerminal.HelpLines(termContentWidth)
				statusLines := 0
				if status != "" {
					statusLines = 1
				}
				maxHelpHeight := termContentHeight - statusLines - tabBarHeight
				if maxHelpHeight < 0 {
					maxHelpHeight = 0
				}
				if len(helpLines) > maxHelpHeight {
					helpLines = helpLines[:maxHelpHeight]
				}
				maxTermHeight := termContentHeight - statusLines - len(helpLines) - tabBarHeight
				if maxTermHeight < 0 {
					maxTermHeight = 0
				}
				if termH > maxTermHeight {
					termH = maxTermHeight
				}
				canvas.Compose(&compositor.PositionedVTermLayer{
					VTermLayer: termLayer,
					PosX:       originX,
					PosY:       originY,
					Width:      termW,
					Height:     termH,
				})
				if status != "" {
					if statusDrawable := a.sidebarBottomStatus.get(status, originX, originY+termH); statusDrawable != nil {
						canvas.Compose(statusDrawable)
					}
				}
				if len(helpLines) > 0 {
					helpContent := clampLines(strings.Join(helpLines, "\n"), termContentWidth, len(helpLines))
					helpY := originY + termContentHeight - len(helpLines) - tabBarHeight
					if helpDrawable := a.sidebarBottomHelp.get(helpContent, originX, helpY); helpDrawable != nil {
						canvas.Compose(helpDrawable)
					}
				} else if status == "" && termContentHeight > termH+tabBarHeight {
					blank := strings.Repeat(" ", termContentWidth)
					if blankDrawable := a.sidebarBottomHelp.get(blank, originX, originY+termContentHeight-1-tabBarHeight); blankDrawable != nil {
						canvas.Compose(blankDrawable)
					}
				}
			} else {
				bottomContent := clampLines(a.sidebarTerminal.View(), termContentWidth, termContentHeight)
				if bottomDrawable := a.sidebarBottomContent.get(bottomContent, centerX+2, termPaneY+1); bottomDrawable != nil {
					canvas.Compose(bottomDrawable)
				}
			}
			for _, border := range a.sidebarBottomBorders.get(centerX, termPaneY, centerWidth, centerBottomHeight) {
				canvas.Compose(border)
			}
		}
	}

	// Sidebar pane (rightmost) - full height
	if a.layout.ShowSidebar() {
		sidebarX := leftGutter + a.layout.DashboardWidth()
		if a.layout.ShowCenter() {
			sidebarX += a.layout.GapX() + a.layout.CenterWidth()
		}
		sidebarX += a.layout.GapX()
		sidebarWidth := a.layout.SidebarWidth()
		sidebarHeight := a.layout.Height()
		contentWidth := sidebarWidth - 4
		if contentWidth < 1 {
			contentWidth = 1
		}
		sidebarContentHeight := sidebarHeight - 2
		if sidebarContentHeight < 1 {
			sidebarContentHeight = 1
		}

		tabBar := a.sidebar.TabBarView()
		tabBarHeight := 0
		if tabBar != "" {
			tabBarHeight = 1
			tabBarContent := clampLines(tabBar, contentWidth, 1)
			if tabBarDrawable := a.sidebarTopTabBar.get(tabBarContent, sidebarX+2, topGutter+1); tabBarDrawable != nil {
				canvas.Compose(tabBarDrawable)
			}
		}

		innerContentHeight := sidebarContentHeight - tabBarHeight
		if innerContentHeight < 1 {
			innerContentHeight = 1
		}
		termX := sidebarX + 2
		termY := topGutter + 1 + tabBarHeight
		sidebarTopOwnsCursor := a.focusedPane == messages.PaneSidebar
		if termLayer := a.sidebar.TerminalLayerWithCursorOwner(sidebarTopOwnsCursor); termLayer != nil {
			canvas.Compose(&compositor.PositionedVTermLayer{
				VTermLayer: termLayer,
				PosX:       termX,
				PosY:       termY,
				Width:      contentWidth,
				Height:     innerContentHeight,
			})
		} else {
			topContent := clampLines(a.sidebar.ContentView(), contentWidth, innerContentHeight)
			if topDrawable := a.sidebarTopContent.get(topContent, termX, termY); topDrawable != nil {
				canvas.Compose(topDrawable)
			}
		}
		for _, border := range a.sidebarTopBorders.get(sidebarX, topGutter, sidebarWidth, sidebarHeight) {
			canvas.Compose(border)
		}
	}

	// Overlay layers (dialogs, toasts, etc.)
	a.composeOverlays(canvas)

	view.SetContent(syncBegin + canvas.Render() + syncEnd)
	view.Cursor = a.overlayCursor()
	return view
}
