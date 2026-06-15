package sidebar

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// seededTabModel returns a model bound to a workspace with `count` in-memory
// tabs (no PTY, zero-size so refreshTerminalSize is a no-op) and the active tab
// reset to 0. None of the helpers it exercises exec an external process.
func seededTabModel(t *testing.T, count int) *TerminalModel {
	t.Helper()
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	tabs := make([]*TerminalTab, count)
	for i := range tabs {
		tabs[i] = newWorkspaceTab(t, "Terminal")
	}
	if count > 0 {
		seedTabs(t, m, tabs...)
	}
	return m
}

// cellRegion is a hit region covering a single cell at (x,0) used to make a
// click land on a synthetic tab hit deterministically.
func cellRegion(x int) common.HitRegion {
	return common.HitRegion{X: x, Y: 0, Width: 1, Height: 1}
}

func leftClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestScreenToTerminalFallback(t *testing.T) {
	m := &TerminalModel{
		width:   10,
		height:  5,
		offsetX: 2,
		offsetY: 1,
	}

	x, y, in := m.screenToTerminal(3, 3)
	if x != 1 || y != 1 || !in {
		t.Fatalf("expected (1,1) in bounds, got (%d,%d) in=%v", x, y, in)
	}

	_, _, in = m.screenToTerminal(20, 3)
	if in {
		t.Fatalf("expected out of bounds for large x")
	}
}

func TestScreenToTerminalWithVTerm(t *testing.T) {
	wt := &data.Workspace{Repo: "/repo", Root: "/repo/wt"}
	m := NewTerminalModel()
	m.workspace = wt
	wtID := string(wt.ID())
	m.tabs.ByWorkspace[wtID] = []*TerminalTab{
		{
			ID:    "test-tab",
			Name:  "Terminal 1",
			State: &TerminalState{VTerm: vterm.New(4, 3)},
		},
	}
	m.tabs.ActiveByWorkspace[wtID] = 0
	m.offsetX = 1
	m.offsetY = 1

	// With tabs, Y is offset by tabBarHeight (1)
	x, y, in := m.screenToTerminal(4, 3)
	if x != 3 || y != 1 || !in {
		t.Fatalf("expected (3,1) in bounds, got (%d,%d) in=%v", x, y, in)
	}

	_, _, in = m.screenToTerminal(5, 3)
	if in {
		t.Fatalf("expected out of bounds for x beyond vterm width")
	}
}

func TestSetOffset(t *testing.T) {
	tests := []struct {
		name string
		x, y int
	}{
		{name: "origin", x: 0, y: 0},
		{name: "positive offset", x: 12, y: 3},
		{name: "negative offset is stored verbatim", x: -4, y: -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTerminalModel()
			m.SetOffset(tt.x, tt.y)
			if m.offsetX != tt.x || m.offsetY != tt.y {
				t.Fatalf("SetOffset(%d,%d) stored (%d,%d)", tt.x, tt.y, m.offsetX, m.offsetY)
			}
		})
	}
}

func TestSetOffsetOverwrites(t *testing.T) {
	m := NewTerminalModel()
	m.SetOffset(5, 6)
	m.SetOffset(1, 2)
	if m.offsetX != 1 || m.offsetY != 2 {
		t.Fatalf("expected last SetOffset to win, got (%d,%d)", m.offsetX, m.offsetY)
	}
}

func TestSetOffsetShiftsScreenToTerminal(t *testing.T) {
	// SetOffset is the seam that screenToTerminal subtracts. Moving the offset
	// must move where a given screen coordinate lands in the fallback bounds path.
	m := &TerminalModel{width: 10, height: 5}
	m.SetOffset(2, 1)
	x, y, in := m.screenToTerminal(3, 3)
	if x != 1 || y != 1 || !in {
		t.Fatalf("expected (1,1) in bounds after SetOffset(2,1), got (%d,%d) in=%v", x, y, in)
	}

	// Re-offsetting changes the translation for the same screen point.
	m.SetOffset(0, 0)
	x, y, in = m.screenToTerminal(3, 3)
	if x != 3 || y != 2 || !in {
		t.Fatalf("expected (3,2) after SetOffset(0,0), got (%d,%d) in=%v", x, y, in)
	}
}

func TestCloseTabAtOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		idx  int
	}{
		{name: "negative index", idx: -1},
		{name: "index past end", idx: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := seededTabModel(t, 2)

			gotM, cmd := m.closeTabAt(tt.idx)

			if gotM != m {
				t.Fatal("expected the same model returned")
			}
			if cmd != nil {
				t.Fatal("expected nil command for out-of-range index")
			}
			if got := len(m.getTabs()); got != 2 {
				t.Fatalf("expected tab list untouched, got %d tabs", got)
			}
		})
	}
}

func TestCloseTabAtNoTabsIsNoop(t *testing.T) {
	m := seededTabModel(t, 0)
	_, cmd := m.closeTabAt(0)
	if cmd != nil {
		t.Fatal("expected nil command when there are no tabs")
	}
	if got := len(m.getTabs()); got != 0 {
		t.Fatalf("expected no tabs, got %d", got)
	}
}

func TestCloseTabAtRemovesTabAndTearsDownState(t *testing.T) {
	m := seededTabModel(t, 2)
	tabs := m.getTabs()
	first := tabs[0]
	second := tabs[1]
	first.State.Running = true
	first.State.RestartBackoff = 4

	gotM, cmd := m.closeTabAt(0)

	if gotM != m {
		t.Fatal("expected the same model returned")
	}
	// No SessionName on the closed tab -> no GC command.
	if cmd != nil {
		t.Fatal("expected nil command when the closed tab has no session name")
	}
	remaining := m.getTabs()
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining tab, got %d", len(remaining))
	}
	if remaining[0] != second {
		t.Fatal("expected the second tab to survive")
	}
	first.State.mu.Lock()
	running := first.State.Running
	backoff := first.State.RestartBackoff
	first.State.mu.Unlock()
	if running {
		t.Fatal("expected closed tab Running flag cleared")
	}
	if backoff != 0 {
		t.Fatalf("expected closed tab RestartBackoff reset to 0, got %d", backoff)
	}
}

func TestCloseTabAtWithSessionNameReturnsCleanupCommand(t *testing.T) {
	m := seededTabModel(t, 1)
	m.getTabs()[0].State.SessionName = "amux-session-xyz"

	_, cmd := m.closeTabAt(0)

	// A non-empty session name yields a GC command (its tmux exec only runs if
	// the runtime later invokes the closure, which the test never does).
	if cmd == nil {
		t.Fatal("expected a cleanup command for a tab with a session name")
	}
	if got := len(m.getTabs()); got != 0 {
		t.Fatalf("expected the tab removed, got %d tabs", got)
	}
}

func TestCloseTabAtAdjustsActiveIndex(t *testing.T) {
	tests := []struct {
		name      string
		tabs      int
		active    int
		closeIdx  int
		wantLen   int
		wantActiv int
	}{
		{
			name: "closing only tab resets active to 0",
			tabs: 1, active: 0, closeIdx: 0, wantLen: 0, wantActiv: 0,
		},
		{
			name: "closing active last tab clamps to new last",
			tabs: 3, active: 2, closeIdx: 2, wantLen: 2, wantActiv: 1,
		},
		{
			name: "closing tab before active shifts active down",
			tabs: 3, active: 2, closeIdx: 0, wantLen: 2, wantActiv: 1,
		},
		{
			name: "closing tab after active leaves active put",
			tabs: 3, active: 0, closeIdx: 2, wantLen: 2, wantActiv: 0,
		},
		{
			name: "closing the active middle tab keeps the index",
			tabs: 3, active: 1, closeIdx: 1, wantLen: 2, wantActiv: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := seededTabModel(t, tt.tabs)
			m.setActiveTabIdx(tt.active)

			m.closeTabAt(tt.closeIdx)

			if got := len(m.getTabs()); got != tt.wantLen {
				t.Fatalf("expected %d tabs, got %d", tt.wantLen, got)
			}
			if got := m.getActiveTabIdx(); got != tt.wantActiv {
				t.Fatalf("expected active idx %d, got %d", tt.wantActiv, got)
			}
		})
	}
}

func TestHandleTabBarClickOutsideBar(t *testing.T) {
	tests := []struct {
		name string
		y    int
	}{
		{name: "above bar", y: -1},
		{name: "below bar", y: tabBarHeight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := seededTabModel(t, 2)
			m.tabHits = []terminalTabHit{
				{kind: terminalTabHitTab, index: 1, region: cellRegion(0)},
			}
			// Offset 0, so screen Y maps directly to local Y.
			_, cmd := m.handleTabBarClick(leftClick(0, tt.y))
			if cmd != nil {
				t.Fatal("expected nil command for a click outside the tab bar row")
			}
			if got := m.getActiveTabIdx(); got != 0 {
				t.Fatalf("expected active tab unchanged, got %d", got)
			}
		})
	}
}

func TestHandleTabBarClickSelectsTab(t *testing.T) {
	m := seededTabModel(t, 3)
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitTab, index: 0, region: cellRegion(0)},
		{kind: terminalTabHitTab, index: 2, region: cellRegion(1)},
	}

	_, cmd := m.handleTabBarClick(leftClick(1, 0))

	if cmd != nil {
		t.Fatal("expected tab selection to return no command")
	}
	if got := m.getActiveTabIdx(); got != 2 {
		t.Fatalf("expected active tab idx 2 after clicking its hit, got %d", got)
	}
}

func TestHandleTabBarClickHonorsOffset(t *testing.T) {
	// Hit regions are recorded in view-local coordinates; a non-zero offset must
	// be subtracted from the incoming screen coordinates before testing hits.
	m := seededTabModel(t, 2)
	m.SetOffset(5, 2)
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitTab, index: 1, region: cellRegion(0)},
	}

	// Screen (5,2) -> local (0,0), which lands on the tab hit.
	_, cmd := m.handleTabBarClick(leftClick(5, 2))
	if cmd != nil {
		t.Fatal("expected nil command for a plain tab selection")
	}
	if got := m.getActiveTabIdx(); got != 1 {
		t.Fatalf("expected active tab idx 1 after offset-adjusted click, got %d", got)
	}
}

func TestHandleTabBarClickCloseButton(t *testing.T) {
	m := seededTabModel(t, 2)
	survivor := m.getTabs()[1]
	// Close button overlaps the tab region; closes are checked first.
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitTab, index: 0, region: cellRegion(0)},
		{kind: terminalTabHitClose, index: 0, region: cellRegion(0)},
	}

	_, cmd := m.handleTabBarClick(leftClick(0, 0))

	// No session name on the closed tab, so no cleanup command is returned.
	if cmd != nil {
		t.Fatal("expected nil command when closing a sessionless tab")
	}
	tabs := m.getTabs()
	if len(tabs) != 1 {
		t.Fatalf("expected the tab closed, got %d tabs", len(tabs))
	}
	if tabs[0] != survivor {
		t.Fatal("expected the second tab to survive the close click")
	}
}

func TestHandleTabBarClickCloseOutOfRangeIndex(t *testing.T) {
	// A stale close hit whose index no longer maps to a tab must be ignored
	// rather than panic or close the wrong tab.
	m := seededTabModel(t, 1)
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitClose, index: 7, region: cellRegion(0)},
	}

	_, cmd := m.handleTabBarClick(leftClick(0, 0))

	if cmd != nil {
		t.Fatal("expected nil command for an out-of-range close hit")
	}
	if got := len(m.getTabs()); got != 1 {
		t.Fatalf("expected the tab list untouched, got %d tabs", got)
	}
}

func TestHandleTabBarClickPlusCreatesTab(t *testing.T) {
	m := seededTabModel(t, 1)
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitPlus, index: 0, region: cellRegion(0)},
	}

	_, cmd := m.handleTabBarClick(leftClick(0, 0))

	// The "+" button returns CreateNewTab's command (a closure that spawns a PTY
	// only when later run by the runtime). We assert the command exists but never
	// invoke it.
	if cmd == nil {
		t.Fatal("expected a creation command from the plus button")
	}
}

func TestHandleTabBarClickPlusWithoutWorkspace(t *testing.T) {
	// CreateNewTab short-circuits to a nil command when no workspace is bound, so
	// clicking the plus button must yield a nil command.
	m := NewTerminalModel()
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitPlus, index: 0, region: cellRegion(0)},
	}

	_, cmd := m.handleTabBarClick(leftClick(0, 0))
	if cmd != nil {
		t.Fatal("expected nil command from plus button without a workspace")
	}
}

func TestHandleTabBarClickMiss(t *testing.T) {
	// A click inside the bar row that matches no hit region is a no-op.
	m := seededTabModel(t, 2)
	m.tabHits = []terminalTabHit{
		{kind: terminalTabHitTab, index: 1, region: cellRegion(0)},
	}

	_, cmd := m.handleTabBarClick(leftClick(9, 0))

	if cmd != nil {
		t.Fatal("expected nil command for a click that misses every hit region")
	}
	if got := m.getActiveTabIdx(); got != 0 {
		t.Fatalf("expected active tab unchanged on a miss, got %d", got)
	}
}

func TestHandleTabBarClickNoHits(t *testing.T) {
	// Empty tabHits (e.g. before the first render) must not panic.
	m := seededTabModel(t, 1)
	m.tabHits = nil

	_, cmd := m.handleTabBarClick(leftClick(0, 0))
	if cmd != nil {
		t.Fatal("expected nil command when there are no recorded hits")
	}
}
