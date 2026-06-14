package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/vterm"
)

// newThreePaneApp builds an App wired with real (empty) pane models and a
// three-pane layout, matching the construction used by the sibling mouse tests.
// It mirrors how the production App routes pointer events without standing up a
// live Bubble Tea program or any external tmux/git process.
func newThreePaneApp(t *testing.T) *App {
	t.Helper()
	l := layout.NewManager()
	l.Resize(140, 40)
	if !l.ShowCenter() || !l.ShowSidebar() {
		t.Fatalf("expected three-pane layout at 140x40")
	}
	app := &App{
		width:           140,
		height:          40,
		layout:          l,
		dashboard:       dashboard.New(),
		center:          center.New(&config.Config{}),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()
	return app
}

// newSelectableCenterApp builds a three-pane App whose center pane holds one tab
// with a populated virtual terminal, so a left-drag selection has real content to
// extend. It returns the App and the tab's terminal for observing mutated
// selection state (HasSelection/SelectedText) after routing pointer events.
func newSelectableCenterApp(t *testing.T) (*App, *vterm.VTerm) {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	l := layout.NewManager()
	l.Resize(140, 40)
	if !l.ShowCenter() || !l.ShowSidebar() {
		t.Fatalf("expected three-pane layout at 140x40")
	}

	c := center.New(cfg)
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	c.SetWorkspace(ws)
	term := vterm.New(80, 24)
	for i := 0; i < 40; i++ {
		term.Write([]byte("the quick brown fox jumps over the lazy dog\r\n"))
	}
	c.AddTab(&center.Tab{ID: center.TabID("tab-1"), Workspace: ws, Terminal: term})

	app := &App{
		width:           140,
		height:          40,
		layout:          l,
		dashboard:       dashboard.New(),
		center:          c,
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	app.updateLayout()
	app.focusPane(messages.PaneCenter)
	return app, term
}

// paneAnchor returns an interior point that paneForPoint maps to the requested
// pane, so motion/release routing tests can target each pane deterministically.
func paneAnchor(t *testing.T, l *layout.Manager, pane messages.PaneType) (int, int) {
	t.Helper()
	left := l.LeftGutter()
	top := l.TopGutter()
	dashWidth := l.DashboardWidth()
	gap := l.GapX()
	centerWidth := l.CenterWidth()
	centerStart := left + dashWidth + gap
	sidebarStart := centerStart + centerWidth + gap
	topPaneHeight, _ := sidebarPaneHeights(l.Height())

	switch pane {
	case messages.PaneDashboard:
		return left + 1, top + 1
	case messages.PaneCenter:
		return centerStart + 1, top + 1
	case messages.PaneSidebar:
		return sidebarStart + 1, top + 1
	case messages.PaneSidebarTerminal:
		return sidebarStart + 1, top + topPaneHeight + 1
	default:
		t.Fatalf("unsupported pane anchor: %v", pane)
		return 0, 0
	}
}

func TestHandleMouseMsg_DispatchByMessageType(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{name: "click", msg: tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: 1}},
		{name: "wheel", msg: tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 1, Y: 1}},
		{name: "motion", msg: tea.MouseMotionMsg{Button: tea.MouseLeft, X: 1, Y: 1}},
		{name: "release", msg: tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 1, Y: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newThreePaneApp(t)
			app.focusedPane = messages.PaneDashboard
			// Should route without panicking and leave focus on the dashboard
			// (interior dashboard coordinates do not trigger a focus change).
			_ = app.handleMouseMsg(tc.msg)
			if app.focusedPane != messages.PaneDashboard {
				t.Fatalf("expected focus unchanged, got %v", app.focusedPane)
			}
		})
	}
}

func TestHandleMouseMsg_UnknownMessageReturnsNil(t *testing.T) {
	app := newThreePaneApp(t)
	type unknownMsg struct{}
	if cmd := app.handleMouseMsg(unknownMsg{}); cmd != nil {
		t.Fatalf("expected nil command for non-mouse message, got %T", cmd)
	}
	if cmd := app.handleMouseMsg(nil); cmd != nil {
		t.Fatalf("expected nil command for nil message, got %T", cmd)
	}
}

// TestRouteMouseMotion_LeftDragExtendsFocusedCenterSelectionAcrossPanes proves
// the cross-pane left-drag contract by observing mutated pane state rather than
// focus invariance (which motion never changes). It mirrors
// TestRouteMouseWheel_FocusesHoveredSidebarAndScrollsChanges: it seeds the center
// pane with selectable terminal content, starts a selection with a left-click,
// then sends left-drag motion whose pointer sits over a *different* pane (the
// sidebar). Because left-drag stays bound to the source (focused) pane, the
// center terminal's selection must actually grow; if routing followed the pointer
// the sidebar would receive the motion and the center selection would not change.
func TestRouteMouseMotion_LeftDragExtendsFocusedCenterSelectionAcrossPanes(t *testing.T) {
	app, term := newSelectableCenterApp(t)

	// Anchor a selection with a left-click inside the center pane.
	cx, cy := paneAnchor(t, app.layout, messages.PaneCenter)
	app.routeMouseClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: cx + 5, Y: cy + 2})
	if !term.HasSelection() {
		t.Fatal("expected left-click in center to start a selection")
	}
	beforeText := term.SelectedText()

	// Drag with the pointer parked over the sidebar (a different pane). The motion
	// must still route to the focused center pane and extend its selection.
	sx, sy := paneAnchor(t, app.layout, messages.PaneSidebar)
	app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: sx, Y: sy})

	if !term.HasSelection() {
		t.Fatal("expected cross-pane left-drag to keep the center selection active")
	}
	afterText := term.SelectedText()
	if len(afterText) <= len(beforeText) {
		t.Fatalf("expected cross-pane left-drag to extend the center selection: before=%q after=%q", beforeText, afterText)
	}
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("left-drag motion must not change focus: want center got %v", app.focusedPane)
	}
}

func TestRouteMouseMotion_LeftButtonStaysBoundToFocusedPane(t *testing.T) {
	// Left-drag motion must remain bound to the pane focused on mouse-down even
	// when the pointer travels into (or out of) another pane's region. Motion
	// never mutates keyboard focus, so this loop verifies routing only via the
	// focus-target invariant across every pane; the observable proof that the
	// drag reaches the *source* pane (not the pane under the pointer) lives in
	// TestRouteMouseMotion_LeftDragExtendsFocusedCenterSelectionAcrossPanes.
	for _, focus := range []messages.PaneType{
		messages.PaneDashboard,
		messages.PaneCenter,
		messages.PaneSidebar,
		messages.PaneSidebarTerminal,
	} {
		app := newThreePaneApp(t)
		app.focusedPane = focus

		// Aim the pointer at a different pane than the one focused on mouse-down.
		otherPane := messages.PaneCenter
		if focus == messages.PaneCenter {
			otherPane = messages.PaneSidebar
		}
		x, y := paneAnchor(t, app.layout, otherPane)

		_ = app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: y})
		if app.focusedPane != focus {
			t.Fatalf("left-drag motion must not change focus: want %v got %v", focus, app.focusedPane)
		}
	}
}

func TestRouteMouseMotion_LeftButtonOutOfBoundsStillRoutesToFocusedPane(t *testing.T) {
	// Out-of-bounds left-drag motion must still reach the focused pane (edge
	// scroll / selection extension depends on receiving these events), unlike
	// non-left motion which is dropped when off-target.
	app := newThreePaneApp(t)
	app.focusedPane = messages.PaneCenter

	// A point above the top gutter is outside every pane region.
	cmd := app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 1, Y: -5})
	_ = cmd // command may be nil for an empty center; the contract is "no panic, focus held".
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected out-of-bounds left motion to keep center focus, got %v", app.focusedPane)
	}
}

func TestRouteMouseMotion_NonLeftButtonOffTargetReturnsNil(t *testing.T) {
	app := newThreePaneApp(t)
	app.focusedPane = messages.PaneCenter

	// Non-left motion routes by pointer target; an out-of-pane point yields nil.
	if cmd := app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseNone, X: 1, Y: -5}); cmd != nil {
		t.Fatalf("expected nil for non-left motion off any pane, got %T", cmd)
	}
}

func TestRouteMouseMotion_NoLayoutNonLeftReturnsNil(t *testing.T) {
	// paneForPoint short-circuits when layout is nil, so non-left motion has no
	// target and must be dropped.
	app := &App{}
	if cmd := app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseNone, X: 10, Y: 10}); cmd != nil {
		t.Fatalf("expected nil motion with no layout, got %T", cmd)
	}
}

func TestRouteMouseMotion_NonLeftRoutesByPointerTarget(t *testing.T) {
	// Each pane region should accept routed non-left motion without panicking;
	// motion never changes keyboard focus regardless of target.
	for _, pane := range []messages.PaneType{
		messages.PaneDashboard,
		messages.PaneCenter,
		messages.PaneSidebar,
		messages.PaneSidebarTerminal,
	} {
		app := newThreePaneApp(t)
		app.focusedPane = messages.PaneDashboard
		x, y := paneAnchor(t, app.layout, pane)

		_ = app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseNone, X: x, Y: y})
		if app.focusedPane != messages.PaneDashboard {
			t.Fatalf("motion must not change focus when targeting %v, got %v", pane, app.focusedPane)
		}
	}
}

// TestRouteMouseRelease_LeftDragFinalizesFocusedCenterSelectionAcrossPanes
// proves the cross-pane left-drag *release* contract by observing mutated pane
// state rather than focus invariance (which release never changes). It seeds the
// center pane with selectable terminal content, starts and extends a selection,
// then releases the left button with the pointer parked over a *different* pane
// (the sidebar). Because left-release stays bound to the source (focused) pane,
// the center pane must finalize the selection there: the terminal still holds the
// dragged selection text, and—critically—the selection is now inactive, so a
// follow-up left-drag can no longer extend it. If routing followed the pointer,
// the sidebar would swallow the release, the center selection would remain active,
// and the follow-up drag would keep growing it. This is the observable proof that
// removing routeMouseRelease's left-drag-to-focused-pane binding breaks the
// contract; the sibling focus-invariance loop cannot catch that regression.
func TestRouteMouseRelease_LeftDragFinalizesFocusedCenterSelectionAcrossPanes(t *testing.T) {
	app, term := newSelectableCenterApp(t)

	// Anchor a selection with a left-click inside the center pane.
	cx, cy := paneAnchor(t, app.layout, messages.PaneCenter)
	app.routeMouseClick(tea.MouseClickMsg{Button: tea.MouseLeft, X: cx + 5, Y: cy + 2})
	if !term.HasSelection() {
		t.Fatal("expected left-click in center to start a selection")
	}

	// Extend the selection with a left-drag whose pointer is over the sidebar.
	sx, sy := paneAnchor(t, app.layout, messages.PaneSidebar)
	app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: sx, Y: sy})
	draggedText := term.SelectedText()
	if len(draggedText) == 0 {
		t.Fatalf("expected the cross-pane left-drag to extend the center selection, got %q", draggedText)
	}

	// Release the left button with the pointer still parked over the sidebar. The
	// release must route to the focused center pane and finalize its selection.
	app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: sx, Y: sy})

	// The finalized selection must still reflect the source-pane drag, not a
	// sidebar no-op that left the center selection untouched/unfinalized.
	if !term.HasSelection() {
		t.Fatal("expected the center terminal to retain its finalized selection after release")
	}
	if got := term.SelectedText(); got != draggedText {
		t.Fatalf("finalized selection text changed: drag=%q after-release=%q", draggedText, got)
	}

	// A finalized selection is no longer active, so a further left-drag must NOT
	// grow it. If the release had been misrouted to the sidebar, the center
	// selection would still be active and this drag would extend it again — which
	// is exactly the regression that removing the left-drag-to-focused binding
	// from routeMouseRelease introduces.
	fx, fy := cx+40, cy+10
	app.routeMouseMotion(tea.MouseMotionMsg{Button: tea.MouseLeft, X: fx, Y: fy})
	if after := term.SelectedText(); after != draggedText {
		t.Fatalf("post-release left-drag must not extend a finalized selection: before=%q after=%q", draggedText, after)
	}
}

func TestRouteMouseRelease_LeftButtonStaysBoundToFocusedPane(t *testing.T) {
	// Cross-pane left-drag release must finalize in the source (focused) pane.
	// Release, like motion, never changes keyboard focus, so this loop asserts the
	// focus-target invariant only; the mutated-state proof that left-drag routing
	// follows the source pane rather than the pointer is covered by
	// TestRouteMouseMotion_LeftDragExtendsFocusedCenterSelectionAcrossPanes.
	for _, focus := range []messages.PaneType{
		messages.PaneDashboard,
		messages.PaneCenter,
		messages.PaneSidebar,
		messages.PaneSidebarTerminal,
	} {
		app := newThreePaneApp(t)
		app.focusedPane = focus

		otherPane := messages.PaneCenter
		if focus == messages.PaneCenter {
			otherPane = messages.PaneSidebar
		}
		x, y := paneAnchor(t, app.layout, otherPane)

		_ = app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
		if app.focusedPane != focus {
			t.Fatalf("left release must not change focus: want %v got %v", focus, app.focusedPane)
		}
	}
}

func TestRouteMouseRelease_LeftButtonOutOfBoundsRoutesToFocusedPane(t *testing.T) {
	app := newThreePaneApp(t)
	app.focusedPane = messages.PaneCenter

	_ = app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 1, Y: -5})
	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected out-of-bounds left release to keep center focus, got %v", app.focusedPane)
	}
}

func TestRouteMouseRelease_NonLeftButtonOffTargetReturnsNil(t *testing.T) {
	app := newThreePaneApp(t)
	app.focusedPane = messages.PaneCenter

	if cmd := app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseRight, X: 1, Y: -5}); cmd != nil {
		t.Fatalf("expected nil for non-left release off any pane, got %T", cmd)
	}
}

func TestRouteMouseRelease_NoLayoutNonLeftReturnsNil(t *testing.T) {
	app := &App{}
	if cmd := app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseRight, X: 10, Y: 10}); cmd != nil {
		t.Fatalf("expected nil release with no layout, got %T", cmd)
	}
}

func TestRouteMouseRelease_NonLeftRoutesByPointerTarget(t *testing.T) {
	for _, pane := range []messages.PaneType{
		messages.PaneDashboard,
		messages.PaneCenter,
		messages.PaneSidebar,
		messages.PaneSidebarTerminal,
	} {
		app := newThreePaneApp(t)
		app.focusedPane = messages.PaneDashboard
		x, y := paneAnchor(t, app.layout, pane)

		_ = app.routeMouseRelease(tea.MouseReleaseMsg{Button: tea.MouseRight, X: x, Y: y})
		if app.focusedPane != messages.PaneDashboard {
			t.Fatalf("release must not change focus when targeting %v, got %v", pane, app.focusedPane)
		}
	}
}
