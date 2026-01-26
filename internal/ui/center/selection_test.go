package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

func setupSelectionModel(t *testing.T) (*Model, *Tab) {
	t.Helper()
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	wt := &data.Workspace{
		Name: "wt",
		Repo: "/tmp/repo",
		Root: "/tmp/repo",
	}
	m.SetWorkspace(wt)
	wtID := string(wt.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Workspace: wt,
		Terminal:  vterm.New(80, 24),
	}
	m.tabsByWorkspace[wtID] = []*Tab{tab}
	m.activeTabByWorkspace[wtID] = 0
	m.SetSize(100, 40)
	m.SetOffset(0)
	m.Focus()
	return m, tab
}

func TestSelectionLifecycle(t *testing.T) {
	m, tab := setupSelectionModel(t)

	click := tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft}
	m, _ = m.Update(click)

	termX, termY, inBounds := m.screenToTerminal(10, 10)
	if !inBounds {
		t.Fatalf("expected click to be in bounds")
	}

	// Convert termY to absolute line for comparison
	var expectedStartLine int
	tab.mu.Lock()
	if tab.Terminal != nil {
		expectedStartLine = tab.Terminal.ScreenYToAbsoluteLine(termY)
	}
	if !tab.Selection.Active {
		tab.mu.Unlock()
		t.Fatalf("expected selection to be active after click")
	}
	if tab.Selection.StartX != termX || tab.Selection.StartLine != expectedStartLine {
		tab.mu.Unlock()
		t.Fatalf("unexpected selection start: got (%d,%d), want (%d,%d)", tab.Selection.StartX, tab.Selection.StartLine, termX, expectedStartLine)
	}
	tab.mu.Unlock()

	drag := tea.MouseMotionMsg{X: 14, Y: 12, Button: tea.MouseLeft}
	m, _ = m.Update(drag)

	dragX, dragY, _ := m.screenToTerminal(14, 12)
	tab.mu.Lock()
	var expectedEndLine int
	if tab.Terminal != nil {
		expectedEndLine = tab.Terminal.ScreenYToAbsoluteLine(dragY)
	}
	if tab.Selection.EndX != dragX || tab.Selection.EndLine != expectedEndLine {
		tab.mu.Unlock()
		t.Fatalf("unexpected selection end: got (%d,%d), want (%d,%d)", tab.Selection.EndX, tab.Selection.EndLine, dragX, expectedEndLine)
	}
	tab.mu.Unlock()

	release := tea.MouseReleaseMsg{X: 14, Y: 12, Button: tea.MouseLeft}
	_, _ = m.Update(release)

	tab.mu.Lock()
	if tab.Selection.Active {
		tab.mu.Unlock()
		t.Fatalf("expected selection to be inactive after release")
	}
	tab.mu.Unlock()
}

func TestSelectionIgnoredWhenUnfocused(t *testing.T) {
	m, tab := setupSelectionModel(t)
	m.Blur()

	click := tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft}
	_, _ = m.Update(click)

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Selection.Active {
		t.Fatalf("expected selection to remain inactive when unfocused")
	}
}

func TestSelectionClearsOutsideBounds(t *testing.T) {
	m, tab := setupSelectionModel(t)

	click := tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}
	_, _ = m.Update(click)

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Selection.Active {
		t.Fatalf("expected selection to be inactive when clicking outside bounds")
	}
	if tab.Selection.StartX != 0 || tab.Selection.StartLine != 0 || tab.Selection.EndX != 0 || tab.Selection.EndLine != 0 {
		t.Fatalf("expected selection to be cleared when clicking outside bounds, got %+v", tab.Selection)
	}
}

func TestTabBarClickPlusButton(t *testing.T) {
	// This tests that clicking the + button in the tab bar triggers the right action
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m := New(cfg)
	wt := &data.Workspace{
		Name: "wt",
		Repo: "/tmp/repo",
		Root: "/tmp/repo",
	}
	m.SetWorkspace(wt)
	wtID := string(wt.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Workspace: wt,
		Terminal:  vterm.New(80, 24),
		Name:      "claude",
	}
	m.tabsByWorkspace[wtID] = []*Tab{tab}
	m.activeTabByWorkspace[wtID] = 0
	m.SetSize(100, 40)
	m.SetOffset(20) // Simulate dashboard width of 20
	m.Focus()

	// Render to populate tab hits
	_ = m.View()

	t.Logf("Tab hits (%d):", len(m.tabHits))
	for i, hit := range m.tabHits {
		t.Logf("  [%d]: kind=%d index=%d region=(%d,%d,%d,%d)",
			i, hit.kind, hit.index, hit.region.X, hit.region.Y, hit.region.Width, hit.region.Height)
	}

	// Find the plus button hit
	var plusHit *tabHit
	for i := range m.tabHits {
		if m.tabHits[i].kind == tabHitPlus {
			plusHit = &m.tabHits[i]
			break
		}
	}
	if plusHit == nil {
		t.Fatalf("No plus button found in tab hits")
	}

	// Calculate screen coordinates for clicking the plus button
	// The tab bar is at Y=1 (Y=0 is pane border, Y=1 is tab content - compact, no tab border)
	// Content X = offsetX + borderLeft(1) + paddingLeft(1) + localX
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)
	screenX := m.offsetX + borderLeft + paddingLeft + plusHit.region.X + 1 // +1 to be inside the button
	screenY := borderTop
	t.Logf("Clicking plus button at screen (%d,%d), local (%d,%d)", screenX, screenY, plusHit.region.X, plusHit.region.Y)

	click := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := m.Update(click)

	if cmd == nil {
		t.Fatalf("Expected command from clicking plus button, got nil")
	}
}
