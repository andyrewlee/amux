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
	wt := &data.Worktree{
		Name: "wt",
		Repo: "/tmp/repo",
		Root: "/tmp/repo",
	}
	m.SetWorktree(wt)
	wtID := string(wt.ID())
	tab := &Tab{
		ID:       TabID("tab-1"),
		Worktree: wt,
		Terminal: vterm.New(80, 24),
	}
	m.tabsByWorktree[wtID] = []*Tab{tab}
	m.activeTabByWorktree[wtID] = 0
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

	tab.mu.Lock()
	if !tab.Selection.Active {
		tab.mu.Unlock()
		t.Fatalf("expected selection to be active after click")
	}
	if tab.Selection.StartX != termX || tab.Selection.StartY != termY {
		tab.mu.Unlock()
		t.Fatalf("unexpected selection start: got (%d,%d), want (%d,%d)", tab.Selection.StartX, tab.Selection.StartY, termX, termY)
	}
	tab.mu.Unlock()

	drag := tea.MouseMotionMsg{X: 14, Y: 12, Button: tea.MouseLeft}
	m, _ = m.Update(drag)

	dragX, dragY, _ := m.screenToTerminal(14, 12)
	tab.mu.Lock()
	if tab.Selection.EndX != dragX || tab.Selection.EndY != dragY {
		tab.mu.Unlock()
		t.Fatalf("unexpected selection end: got (%d,%d), want (%d,%d)", tab.Selection.EndX, tab.Selection.EndY, dragX, dragY)
	}
	tab.mu.Unlock()

	release := tea.MouseReleaseMsg{X: 14, Y: 12, Button: tea.MouseLeft}
	m, _ = m.Update(release)

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
	m, _ = m.Update(click)

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Selection.Active {
		t.Fatalf("expected selection to remain inactive when unfocused")
	}
}

func TestSelectionClearsOutsideBounds(t *testing.T) {
	m, tab := setupSelectionModel(t)

	click := tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}
	m, _ = m.Update(click)

	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Selection.Active {
		t.Fatalf("expected selection to be inactive when clicking outside bounds")
	}
	if tab.Selection.StartX != 0 || tab.Selection.StartY != 0 || tab.Selection.EndX != 0 || tab.Selection.EndY != 0 {
		t.Fatalf("expected selection to be cleared when clicking outside bounds, got %+v", tab.Selection)
	}
}
