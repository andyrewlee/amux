package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
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

func TestRouteMouseWheel_PrefixPaletteConsumesWheel(t *testing.T) {
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

	sidebarStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX() + l.CenterWidth() + l.GapX()
	_, paletteHeight := viewDimensions(app.renderPrefixPalette())
	if paletteHeight <= 0 {
		t.Fatal("expected prefix palette to render a non-zero height")
	}
	y := app.height - paletteHeight
	if y < l.TopGutter() {
		y = l.TopGutter()
	}

	cmd := app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      sidebarStartX + 3,
		Y:      y,
	})
	if cmd != nil {
		t.Fatal("expected palette wheel input to be consumed without command")
	}
	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected focus to remain dashboard, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_FocusesHoveredSidebarAndScrollsChanges(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()

	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	app.sidebar.SetWorkspace(ws)
	status := &git.StatusResult{Clean: false}
	for i := 0; i < 40; i++ {
		status.Unstaged = append(status.Unstaged, git.Change{
			Path: fmt.Sprintf("file-%02d.txt", i),
			Kind: git.ChangeModified,
		})
	}
	app.sidebar.SetGitStatus(status)

	app.focusPane(messages.PaneCenter)

	before := app.sidebar.ContentView()
	if strings.Contains(before, "file-20.txt") {
		t.Fatalf("expected file-20.txt to start off-screen")
	}

	sidebarStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX() + l.CenterWidth() + l.GapX()
	wheel := tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      sidebarStartX + 3,
		Y:      l.TopGutter() + 2,
	}
	for i := 0; i < 24; i++ {
		app.routeMouseWheel(wheel)
	}

	after := app.sidebar.ContentView()
	if app.focusedPane != messages.PaneSidebar {
		t.Fatalf("expected wheel to focus sidebar, got %v", app.focusedPane)
	}
	if !strings.Contains(after, "file-20.txt") {
		t.Fatalf("expected hovered sidebar wheel scroll to reveal later files; got view:\n%s", after)
	}
}

func TestRouteMouseWheel_HoverSidebarTerminalSkipsFocusSideEffects(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()

	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	app.sidebarTerminal.SetWorkspacePreview(ws)
	app.focusPane(messages.PaneCenter)

	sidebarStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX() + l.CenterWidth() + l.GapX()
	topPaneHeight, _ := sidebarPaneHeights(l.Height())
	cmd := app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      sidebarStartX + 3,
		Y:      l.TopGutter() + topPaneHeight + 2,
	})
	if cmd != nil {
		t.Fatal("expected empty sidebar terminal hover wheel to avoid terminal-creation command")
	}
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected focus to remain center for empty sidebar terminal, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_HoverCenterPreservesDetachedReattach(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"codex": {Command: "codex"},
		},
	}
	centerModel := center.New(cfg)
	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          centerModel,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()

	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	ws.OpenTabs = []data.TabInfo{{
		Assistant:   "codex",
		Name:        "Codex",
		SessionName: "amux-test-detached",
		Status:      "detached",
	}}
	centerModel.SetWorkspace(ws)
	if cmd := centerModel.RestoreTabsFromWorkspace(ws); cmd != nil {
		t.Fatal("expected detached tab restore to be synchronous")
	}
	app.focusPane(messages.PaneDashboard)

	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	cmd := app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      centerStartX + 3,
		Y:      l.TopGutter() + 2,
	})
	if cmd == nil {
		t.Fatal("expected wheel focus retarget into center to queue detached-tab reattach")
	}
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected wheel to retarget focus to center, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_HoverDashboardDoesNotRetargetFromFocusedPane(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()
	app.focusPane(messages.PaneCenter)

	cmd := app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      l.LeftGutter() + 1,
		Y:      l.TopGutter() + 2,
	})
	if cmd != nil {
		t.Fatal("expected dashboard hover wheel to avoid activating dashboard rows")
	}
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected focus to remain center, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_HoverEmptyCenterDoesNotStealFocus(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()
	app.focusPane(messages.PaneDashboard)

	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	_ = app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      centerStartX + 3,
		Y:      l.TopGutter() + 2,
	})
	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected focus to remain dashboard for empty center hover, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_DialogOverlayPreventsRetarget(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	app := &App{
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		dialog:          common.NewConfirmDialog("quit", "Quit", "Confirm?"),
	}
	app.dialog.Show()
	app.updateLayout()
	app.focusPane(messages.PaneDashboard)

	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	_ = app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      centerStartX + 3,
		Y:      l.TopGutter() + 2,
	})
	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected dialog overlay to preserve dashboard focus, got %v", app.focusedPane)
	}
}

func TestRouteMouseWheel_ToastOverlayPreventsRetarget(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)

	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"codex": {Command: "codex"},
		},
	}
	centerModel := center.New(cfg)
	app := &App{
		width:           140,
		height:          40,
		layout:          l,
		dashboard:       dashboard.New(),
		center:          centerModel,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		toast:           common.NewToastModel(),
	}
	app.updateLayout()

	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	ws.OpenTabs = []data.TabInfo{{
		Assistant:   "codex",
		Name:        "Codex",
		SessionName: "amux-test-toast-detached",
		Status:      "detached",
	}}
	centerModel.SetWorkspace(ws)
	if cmd := centerModel.RestoreTabsFromWorkspace(ws); cmd != nil {
		t.Fatal("expected detached tab restore to be synchronous")
	}
	app.focusPane(messages.PaneDashboard)

	_ = app.toast.ShowInfo(strings.Repeat("toast ", 12))
	toastView := app.toast.View()
	if toastView == "" {
		t.Fatal("expected visible toast")
	}
	toastWidth, toastHeight := viewDimensions(toastView)
	toastX := (app.width - toastWidth) / 2
	toastY := app.height - 2

	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	centerEndX := centerStartX + l.CenterWidth()
	x := toastX
	if x < centerStartX {
		x = centerStartX
	}
	if x >= centerEndX {
		t.Fatal("expected toast to overlap the center pane in test setup")
	}
	y := toastY
	if y >= l.TopGutter()+l.Height() {
		t.Fatal("expected toast to overlap pane height in test setup")
	}
	if !app.toastCoversPoint(x, y) {
		t.Fatal("expected wheel point to land inside toast overlay")
	}
	if toastHeight < 1 {
		t.Fatal("expected toast height to be positive")
	}

	cmd := app.routeMouseWheel(tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
		X:      x,
		Y:      y,
	})
	if cmd != nil {
		t.Fatal("expected toast-covered wheel input to avoid retarget side effects")
	}
	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected toast overlay to preserve dashboard focus, got %v", app.focusedPane)
	}
}
