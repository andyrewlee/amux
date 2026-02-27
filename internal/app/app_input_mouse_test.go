package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/layout"
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

	assertPaneAt(t, app, left-1, top, messages.PaneDashboard, true)
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
