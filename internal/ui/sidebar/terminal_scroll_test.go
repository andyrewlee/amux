package sidebar

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

// setupScrollModel creates a sidebar terminal model with scrollback content
// for testing auto-scroll during selection. The terminal is 80 cols x 24 rows
// with 100 lines of scrollback.
func setupScrollModel(t *testing.T) (*TerminalModel, *TerminalState) {
	t.Helper()
	wt := &data.Workspace{Repo: "/repo", Root: "/repo/wt"}
	m := NewTerminalModel()
	m.workspace = wt
	m.focused = true
	wsID := string(wt.ID())

	vt := vterm.New(80, 24)
	vt.AllowAltScreenScrollback = true
	// Write enough lines to create scrollback
	for i := 0; i < 100; i++ {
		vt.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}

	ts := &TerminalState{
		VTerm:      vt,
		Running:    true,
		lastWidth:  80,
		lastHeight: 24,
	}
	tab := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 1",
		State: ts,
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
	// width=80, height=26 → terminal viewport = 80x24 (minus tabBar=1, statusReserve=1)
	m.width = 80
	m.height = 26
	m.offsetX = 0
	m.offsetY = 0
	return m, ts
}

func TestSelectionScrollTick_DragAboveViewport(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Click inside terminal to start selection
	// Screen Y = offsetY + tabBarHeight + termY → Y=1 is termY=0 (top row)
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)

	ts.mu.Lock()
	if !ts.Selection.Active {
		ts.mu.Unlock()
		t.Fatal("expected selection to be active after click")
	}
	ts.mu.Unlock()

	// Drag above the viewport: screen Y=0 → termY = 0 - tabBarHeight = -1
	motion := tea.MouseMotionMsg{X: 10, Y: 0, Button: tea.MouseLeft}
	m, cmd := m.Update(motion)

	if cmd == nil {
		t.Fatal("expected a tick command from dragging above viewport")
	}

	// Verify scroll state was set correctly
	ts.mu.Lock()
	if ts.selectionScroll.ScrollDir != 1 {
		t.Fatalf("expected ScrollDir=1 (up), got %d", ts.selectionScroll.ScrollDir)
	}
	if !ts.selectionScroll.Active {
		t.Fatal("expected scroll tick loop to be active")
	}
	gen := ts.selectionScroll.Gen
	ts.mu.Unlock()

	// Scroll to a known position first so we can verify the tick scrolls
	ts.mu.Lock()
	ts.VTerm.ScrollViewToBottom()
	scrollBefore, _ := ts.VTerm.GetScrollInfo()
	ts.mu.Unlock()

	// Process the tick message
	tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	_, cmd = m.Update(tickMsg)

	if cmd == nil {
		t.Fatal("expected next tick command after processing scroll tick")
	}

	// Verify viewport scrolled
	ts.mu.Lock()
	scrollAfter, _ := ts.VTerm.GetScrollInfo()
	if scrollAfter <= scrollBefore {
		t.Fatalf("expected scroll offset to increase (up into history), before=%d after=%d",
			scrollBefore, scrollAfter)
	}

	// Verify selection endpoint was updated to top edge
	if ts.Selection.EndLine != ts.VTerm.ScreenYToAbsoluteLine(0) {
		t.Fatalf("expected EndLine to track top edge, got EndLine=%d, topEdgeAbs=%d",
			ts.Selection.EndLine, ts.VTerm.ScreenYToAbsoluteLine(0))
	}
	ts.mu.Unlock()
}

func TestSelectionScrollTick_DragBelowViewport(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Scroll up first so there's room to scroll down
	ts.mu.Lock()
	ts.VTerm.ScrollView(50)
	ts.mu.Unlock()

	// Click inside terminal to start selection
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)

	// Drag below viewport: screen Y = offsetY + tabBarHeight + termHeight
	// With height=26, tabBar=1, statusReserve=1: termHeight=24, so screen Y=25 → termY=24 (out of bounds)
	motion := tea.MouseMotionMsg{X: 10, Y: 25, Button: tea.MouseLeft}
	m, cmd := m.Update(motion)

	if cmd == nil {
		t.Fatal("expected a tick command from dragging below viewport")
	}

	ts.mu.Lock()
	if ts.selectionScroll.ScrollDir != -1 {
		t.Fatalf("expected ScrollDir=-1 (down), got %d", ts.selectionScroll.ScrollDir)
	}
	gen := ts.selectionScroll.Gen
	scrollBefore, _ := ts.VTerm.GetScrollInfo()
	ts.mu.Unlock()

	// Process tick
	tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	_, cmd = m.Update(tickMsg)

	if cmd == nil {
		t.Fatal("expected next tick command")
	}

	ts.mu.Lock()
	scrollAfter, _ := ts.VTerm.GetScrollInfo()
	if scrollAfter >= scrollBefore {
		t.Fatalf("expected scroll offset to decrease (down toward live), before=%d after=%d",
			scrollBefore, scrollAfter)
	}
	ts.mu.Unlock()
}

func TestSelectionScrollTick_StopsOnRelease(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Click and drag above viewport
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)
	motion := tea.MouseMotionMsg{X: 10, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(motion)

	ts.mu.Lock()
	gen := ts.selectionScroll.Gen
	ts.mu.Unlock()

	// Release mouse
	release := tea.MouseReleaseMsg{X: 10, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(release)

	// Process tick with old gen → should be rejected
	tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	_, cmd := m.Update(tickMsg)

	if cmd != nil {
		t.Fatal("expected no tick command after mouse release (gen should be invalidated)")
	}
}

func TestSelectionScrollTick_StopsOnNewClick(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Start selection and drag above
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)
	motion := tea.MouseMotionMsg{X: 10, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(motion)

	ts.mu.Lock()
	gen := ts.selectionScroll.Gen
	ts.mu.Unlock()

	// New click (starts new selection, resets scroll)
	click2 := tea.MouseClickMsg{X: 15, Y: 10, Button: tea.MouseLeft}
	m, _ = m.Update(click2)

	// Old tick should be rejected
	tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	_, cmd := m.Update(tickMsg)

	if cmd != nil {
		t.Fatal("expected no tick command after new click (gen should be invalidated)")
	}
}

func TestSelectionScrollTick_ContinuousScrolling(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Click and drag above viewport
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)
	motion := tea.MouseMotionMsg{X: 10, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(motion)

	ts.mu.Lock()
	gen := ts.selectionScroll.Gen
	ts.mu.Unlock()

	// Simulate 5 consecutive ticks (mouse held still)
	for i := 0; i < 5; i++ {
		tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
		var cmd tea.Cmd
		m, cmd = m.Update(tickMsg)
		if cmd == nil {
			t.Fatalf("tick %d: expected next tick command (continuous scrolling)", i)
		}
	}

	// Verify we've scrolled multiple lines
	ts.mu.Lock()
	offset, _ := ts.VTerm.GetScrollInfo()
	ts.mu.Unlock()
	// Each tick scrolls 1 line, plus the initial motion scroll = 6+ lines
	if offset < 5 {
		t.Fatalf("expected at least 5 lines scrolled after 5 ticks, got offset=%d", offset)
	}
}

func TestSelectionScrollTick_NoTickInBounds(t *testing.T) {
	m, _ := setupScrollModel(t)

	// Click and drag, both in bounds
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)
	motion := tea.MouseMotionMsg{X: 20, Y: 10, Button: tea.MouseLeft}
	_, cmd := m.Update(motion)

	if cmd != nil {
		t.Fatal("expected no tick command when dragging within viewport bounds")
	}
}

func TestSelectionScrollTick_EndpointTracksLastX(t *testing.T) {
	m, ts := setupScrollModel(t)
	wsID := m.workspaceID()
	activeTab := m.getActiveTab()
	tabID := activeTab.ID

	// Click and drag above viewport at X=40
	click := tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft}
	m, _ = m.Update(click)
	motion := tea.MouseMotionMsg{X: 40, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(motion)

	ts.mu.Lock()
	gen := ts.selectionScroll.Gen
	ts.mu.Unlock()

	// Process tick
	tickMsg := SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
	_, _ = m.Update(tickMsg)

	// Verify endpoint X matches the last motion X
	ts.mu.Lock()
	if ts.Selection.EndX != 40 {
		t.Fatalf("expected EndX=40 (last mouse X), got %d", ts.Selection.EndX)
	}
	ts.mu.Unlock()
}
