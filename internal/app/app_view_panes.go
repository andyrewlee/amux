package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// composeDashboardPane draws the leftmost dashboard pane (content + borders).
func (a *App) composeDashboardPane(canvas *lipgloss.Canvas, leftGutter, topGutter int) {
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
	if dashDrawable := a.renderCache.dashboardContent.get(dashContent, leftGutter+1, topGutter+1); dashDrawable != nil {
		canvas.Compose(dashDrawable)
	}
	for _, border := range a.renderCache.dashboardBorders.get(leftGutter, topGutter, dashWidth, dashHeight) {
		canvas.Compose(border)
	}
}

// composeCenterPane draws the center agent pane, using a direct VTerm layer when
// a terminal tab owns the pane and falling back to string rendering otherwise.
func (a *App) composeCenterPane(canvas *lipgloss.Canvas, leftGutter, topGutter, dashWidth int, blockingOverlayVisible bool, setTerminalCursor func(x, y int)) {
	centerX := leftGutter + dashWidth + a.layout.GapX()
	centerWidth := a.layout.CenterWidth()
	centerHeight := a.layout.Height()

	// Check if we can use VTermLayer for direct cell rendering
	centerOwnsCursor := a.focusedPane == messages.PaneCenter && !blockingOverlayVisible
	termLayer := a.center.TerminalLayerWithCursorOwner(centerOwnsCursor)
	if termLayer != nil && a.center.HasTabs() && !a.center.HasDiffViewer() {
		a.composeCenterTerminalLayer(canvas, centerX, topGutter, centerWidth, centerHeight, termLayer, centerOwnsCursor, setTerminalCursor)
		return
	}
	// Fallback to string-based rendering with borders. The content string still
	// has to be rebuilt each frame (it can change — e.g. the diff viewer while
	// scrolling), but it is content-keyed through drawableCache so a stable
	// pane (the common case: unfocused/no-tab center pane renders byte-identical
	// content every frame) reuses the same *StringDrawable and amortizes its
	// one-time cell parse across frames instead of re-parsing on every compose.
	a.renderCache.centerChrome.Invalidate()
	var centerContent string
	if a.center.HasTabs() {
		centerContent = a.center.View()
	} else {
		centerContent = a.renderCenterPaneContent()
	}
	centerView := buildBorderedPane(centerContent, centerWidth, centerHeight)
	if centerDrawable := a.renderCache.centerContent.get(clampPane(centerView, centerWidth, centerHeight), centerX, topGutter); centerDrawable != nil {
		canvas.Compose(centerDrawable)
	}
}

// composeCenterTerminalLayer draws the center pane's direct VTerm layer plus its
// chrome (borders, tab bar, status line, help lines).
func (a *App) composeCenterTerminalLayer(canvas *lipgloss.Canvas, centerX, topGutter, centerWidth, centerHeight int, termLayer *compositor.VTermLayer, centerOwnsCursor bool, setTerminalCursor func(x, y int)) {
	// Get terminal viewport from center model (accounts for borders, tab bar, help lines)
	termOffsetX, termOffsetY, termW, termH := a.center.TerminalViewport()
	termX := centerX + termOffsetX
	termY := topGutter + termOffsetY
	if centerOwnsCursor {
		termLayer = delegateTerminalCursor(termLayer, termX, termY, termW, termH, setTerminalCursor)
	}

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
	for _, border := range a.renderCache.centerBorders.get(centerX, topGutter, centerWidth, centerHeight) {
		canvas.Compose(border)
	}

	contentWidth := a.center.ContentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Tab bar (top of content area).
	tabBar := clampLines(a.center.TabBarView(), contentWidth, termOffsetY-1)
	if tabBarDrawable := a.renderCache.centerTabBar.get(tabBar, termX, topGutter+1); tabBarDrawable != nil {
		canvas.Compose(tabBarDrawable)
	}

	// Status line (directly below terminal content).
	if status := clampLines(a.center.ActiveTerminalStatusLine(), contentWidth, 1); status != "" {
		if statusDrawable := a.renderCache.centerStatus.get(status, termX, termY+termH); statusDrawable != nil {
			canvas.Compose(statusDrawable)
		}
	}

	// Help lines at bottom of pane. The gate skips the help string build
	// entirely while the center model reports the same help version at the
	// same compose geometry; see Model.helpVersion for the dirtiness
	// invariant. helpY depends on topGutter+centerHeight only through their
	// sum, so the sum is what the geometry key carries.
	helpGate := &a.renderCache.centerHelpGate
	helpGeom := [4]int{termX, termY, contentWidth, topGutter + centerHeight}
	if version := a.center.HelpVersion(); !helpGate.clean(version, helpGeom) {
		rendered := false
		if helpLines := a.center.HelpLines(contentWidth); len(helpLines) > 0 {
			helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
			helpY := topGutter + centerHeight - 1 - len(helpLines)
			if helpY > termY {
				rendered = a.renderCache.centerHelp.get(helpContent, termX, helpY) != nil
			}
		}
		helpGate.record(version, helpGeom, rendered)
	}
	if helpGate.rendered {
		if helpDrawable := a.renderCache.centerHelp.drawable; helpDrawable != nil {
			canvas.Compose(helpDrawable)
		}
	}
}

// composeSidebarPane draws the rightmost sidebar (top changes/project pane and
// the bottom terminal pane), delegating the bottom terminal content to
// composeSidebarTerminalPane.
func (a *App) composeSidebarPane(canvas *lipgloss.Canvas, leftGutter, topGutter int, blockingOverlayVisible bool, setTerminalCursor func(x, y int)) {
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
	if bottomPaneHeight <= 0 {
		return
	}
	contentWidth := sidebarWidth - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	if topPaneHeight > 0 {
		a.composeSidebarTopPane(canvas, sidebarX, topGutter, sidebarWidth, topPaneHeight, contentWidth)
	}

	bottomY := topGutter + topPaneHeight
	bottomContentHeight := bottomPaneHeight - 2
	if bottomContentHeight < 1 {
		bottomContentHeight = 1
	}

	a.composeSidebarTerminalPane(canvas, sidebarX, bottomY, contentWidth, bottomContentHeight, blockingOverlayVisible, setTerminalCursor)
	for _, border := range a.renderCache.sidebarBottomBorders.get(sidebarX, bottomY, sidebarWidth, bottomPaneHeight) {
		canvas.Compose(border)
	}
}

// composeSidebarTopPane draws the sidebar's top changes/project pane (tab bar,
// content, and borders).
func (a *App) composeSidebarTopPane(canvas *lipgloss.Canvas, sidebarX, topGutter, sidebarWidth, topPaneHeight, contentWidth int) {
	topContentHeight := topPaneHeight - 2
	if topContentHeight < 1 {
		topContentHeight = 1
	}

	// Sidebar tab bar (Changes/Project tabs). The gate skips the tab bar
	// string build entirely while the sidebar reports the same tab bar
	// version at the same compose geometry; see TabbedSidebar.tabBarVersion
	// for the dirtiness invariant.
	tabBarY := topGutter + 1 // Inside the border
	tabBarGate := &a.renderCache.sidebarTopTabBarGate
	tabBarGeom := [4]int{sidebarX + 2, tabBarY, contentWidth, 1}
	if version := a.sidebar.TabBarVersion(); !tabBarGate.clean(version, tabBarGeom) {
		rendered := false
		if tabBar := a.sidebar.TabBarView(); tabBar != "" {
			tabBarContent := clampLines(tabBar, contentWidth, 1)
			rendered = a.renderCache.sidebarTopTabBar.get(tabBarContent, sidebarX+2, tabBarY) != nil
		}
		tabBarGate.record(version, tabBarGeom, rendered)
	}
	tabBarHeight := 0
	if tabBarGate.rendered {
		tabBarHeight = 1
		if tabBarDrawable := a.renderCache.sidebarTopTabBar.drawable; tabBarDrawable != nil {
			canvas.Compose(tabBarDrawable)
		}
	}

	// Sidebar content (below tab bar)
	sidebarContentHeight := topContentHeight - tabBarHeight
	if sidebarContentHeight < 1 {
		sidebarContentHeight = 1
	}
	topContent := clampLines(a.sidebar.ContentView(), contentWidth, sidebarContentHeight)
	if topDrawable := a.renderCache.sidebarTopContent.get(topContent, sidebarX+2, topGutter+1+tabBarHeight); topDrawable != nil {
		canvas.Compose(topDrawable)
	}
	for _, border := range a.renderCache.sidebarTopBorders.get(sidebarX, topGutter, sidebarWidth, topPaneHeight) {
		canvas.Compose(border)
	}
}

// composeSidebarTerminalPane draws the sidebar's bottom terminal pane content
// (tab bar, terminal layer or string fallback, status, and help lines).
func (a *App) composeSidebarTerminalPane(canvas *lipgloss.Canvas, sidebarX, bottomY, contentWidth, bottomContentHeight int, blockingOverlayVisible bool, setTerminalCursor func(x, y int)) {
	sidebarOwnsCursor := a.focusedPane == messages.PaneSidebarTerminal && !blockingOverlayVisible
	termLayer := a.sidebarTerminal.TerminalLayerWithCursorOwner(sidebarOwnsCursor)
	if termLayer == nil {
		bottomContent := clampLines(a.sidebarTerminal.View(), contentWidth, bottomContentHeight)
		if bottomDrawable := a.renderCache.sidebarBottomContent.get(bottomContent, sidebarX+2, bottomY+1); bottomDrawable != nil {
			canvas.Compose(bottomDrawable)
		}
		return
	}
	originX, originY := a.sidebarTerminal.TerminalOrigin()
	termW, termH := a.sidebarTerminal.TerminalSize()
	if termW > contentWidth {
		termW = contentWidth
	}
	if termH > bottomContentHeight {
		termH = bottomContentHeight
	}

	// Tab bar (above terminal content) - compact single line
	tabBar := a.sidebarTerminal.TabBarView()
	tabBarHeight := 0
	if tabBar != "" {
		tabBarHeight = 1
		tabBarContent := clampLines(tabBar, contentWidth, 1)
		tabBarY := bottomY + 1 // Inside the border
		if tabBarDrawable := a.renderCache.sidebarBottomTabBar.get(tabBarContent, originX, tabBarY); tabBarDrawable != nil {
			canvas.Compose(tabBarDrawable)
		}
	}

	status := clampLines(a.sidebarTerminal.StatusLine(), contentWidth, 1)
	helpLines := a.sidebarTerminal.HelpLines(contentWidth)
	statusLines := 0
	if status != "" {
		statusLines = 1
	}
	maxHelpHeight := bottomContentHeight - statusLines - tabBarHeight
	if maxHelpHeight < 0 {
		maxHelpHeight = 0
	}
	if len(helpLines) > maxHelpHeight {
		helpLines = helpLines[:maxHelpHeight]
	}
	maxTermHeight := bottomContentHeight - statusLines - len(helpLines) - tabBarHeight
	if maxTermHeight < 0 {
		maxTermHeight = 0
	}
	if termH > maxTermHeight {
		termH = maxTermHeight
	}
	if sidebarOwnsCursor {
		termLayer = delegateTerminalCursor(termLayer, originX, originY, termW, termH, setTerminalCursor)
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
		if statusDrawable := a.renderCache.sidebarBottomStatus.get(status, originX, originY+termH); statusDrawable != nil {
			canvas.Compose(statusDrawable)
		}
	}

	if len(helpLines) > 0 {
		helpContent := clampLines(strings.Join(helpLines, "\n"), contentWidth, len(helpLines))
		helpY := originY + bottomContentHeight - len(helpLines) - tabBarHeight
		if helpDrawable := a.renderCache.sidebarBottomHelp.get(helpContent, originX, helpY); helpDrawable != nil {
			canvas.Compose(helpDrawable)
		}
	} else if status == "" && bottomContentHeight > termH+tabBarHeight {
		blank := strings.Repeat(" ", contentWidth)
		if blankDrawable := a.renderCache.sidebarBottomHelp.get(blank, originX, originY+bottomContentHeight-1-tabBarHeight); blankDrawable != nil {
			canvas.Compose(blankDrawable)
		}
	}
}

// delegateTerminalCursor moves a terminal pane's in-snapshot cursor onto the
// hardware cursor (via setCursor) when it is visible and in bounds, returning a
// layer whose snapshot has ShowCursor=false so exactly one cursor renders. The
// shallow snapshot copy is intentional: only ShowCursor changes; the screen data
// stays read-only for rendering. Returns termLayer unchanged when there is no
// visible cursor to delegate.
func delegateTerminalCursor(termLayer *compositor.VTermLayer, originX, originY, termW, termH int, setCursor func(x, y int)) *compositor.VTermLayer {
	if termLayer == nil || termLayer.Snap == nil {
		return termLayer
	}
	snap := termLayer.Snap
	if snap.ShowCursor && !snap.CursorHidden && snap.ViewOffset == 0 &&
		snap.CursorX >= 0 && snap.CursorY >= 0 &&
		snap.CursorX < termW && snap.CursorY < termH {
		setCursor(originX+snap.CursorX, originY+snap.CursorY)
		snapCopy := *snap
		snapCopy.ShowCursor = false
		termLayer = compositor.NewVTermLayer(&snapCopy)
	}
	return termLayer
}
