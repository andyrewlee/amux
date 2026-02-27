package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestPaneForPoint_NoLayoutReturnsNoMatch(t *testing.T) {
	app := &App{}
	pane, ok := app.paneForPoint(10, 10)
	if ok {
		t.Fatalf("expected no match when layout is nil")
	}
	if pane != paneNone {
		t.Fatalf("expected paneNone, got %v", pane)
	}
}

func TestPaneForPoint_ThreePaneGeometry(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)
	if !l.ShowCenter() || !l.ShowSidebar() {
		t.Fatalf("expected three-pane layout at 140x40")
	}

	app := &App{layout: l}
	left := l.LeftGutter()
	top := l.TopGutter()
	height := l.Height()
	dashWidth := l.DashboardWidth()
	gap := l.GapX()
	centerWidth := l.CenterWidth()
	sidebarWidth := l.SidebarWidth()

	centerStart := left + dashWidth + gap
	sidebarStart := centerStart + centerWidth + gap
	topPaneHeight, _ := sidebarPaneHeights(height)

	assertPaneAt(t, app, left, top-1, paneNone, false)
	assertPaneAt(t, app, left, top+height, paneNone, false)

	assertPaneAt(t, app, left-1, top, paneNone, false)
	assertPaneAt(t, app, left+dashWidth-1, top, messages.PaneDashboard, true)
	assertPaneAt(t, app, left+dashWidth, top, paneNone, false)

	assertPaneAt(t, app, centerStart, top, messages.PaneCenter, true)
	assertPaneAt(t, app, centerStart+centerWidth-1, top, messages.PaneCenter, true)
	assertPaneAt(t, app, centerStart+centerWidth, top, paneNone, false)

	assertPaneAt(t, app, sidebarStart, top, messages.PaneSidebar, true)
	assertPaneAt(t, app, sidebarStart, top+topPaneHeight, messages.PaneSidebarTerminal, true)
	assertPaneAt(t, app, sidebarStart+sidebarWidth, top, paneNone, false)
}

func TestPaneForPoint_TwoPaneNoSidebar(t *testing.T) {
	l := layout.NewManager()
	l.Resize(100, 30)
	if !l.ShowCenter() || l.ShowSidebar() {
		t.Fatalf("expected two-pane layout at 100x30")
	}

	app := &App{layout: l}
	left := l.LeftGutter()
	top := l.TopGutter()
	dashWidth := l.DashboardWidth()
	gap := l.GapX()
	centerWidth := l.CenterWidth()
	centerStart := left + dashWidth + gap

	assertPaneAt(t, app, centerStart, top, messages.PaneCenter, true)
	assertPaneAt(t, app, centerStart+centerWidth, top, paneNone, false)
}

func assertPaneAt(t *testing.T, app *App, x, y int, want messages.PaneType, wantOK bool) {
	t.Helper()
	gotPane, gotOK := app.paneForPoint(x, y)
	if gotOK != wantOK || gotPane != want {
		t.Fatalf("paneForPoint(%d, %d) = (%v, %t), want (%v, %t)", x, y, gotPane, gotOK, want, wantOK)
	}
}

func TestPrefixPaletteContainsPoint(t *testing.T) {
	app := &App{
		prefixActive: true,
		width:        120,
		height:       40,
	}

	if !app.prefixPaletteContainsPoint(10, 39) {
		t.Fatal("expected point in bottom overlay area to hit prefix palette")
	}
	if app.prefixPaletteContainsPoint(10, 0) {
		t.Fatal("expected point outside bottom overlay area not to hit prefix palette")
	}
}

func TestRouteMouseClick_PrefixPaletteConsumesClicks(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		prefixActive:    true,
		width:           140,
		height:          40,
		layout:          l,
		focusedPane:     messages.PaneDashboard,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}

	_, paletteHeight := viewDimensions(app.renderPrefixPalette())
	if paletteHeight <= 0 {
		t.Fatal("expected prefix palette to render a non-zero height")
	}
	y := app.height - paletteHeight
	if y < l.TopGutter() {
		y = l.TopGutter()
	}
	x := l.LeftGutter() + 1

	cmd := app.routeMouseClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	if cmd != nil {
		t.Fatal("expected palette click to be consumed without command")
	}
	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected focus to remain dashboard, got %v", app.focusedPane)
	}
}
