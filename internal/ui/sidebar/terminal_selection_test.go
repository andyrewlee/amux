package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

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
	wt := &data.Worktree{Repo: "/repo", Root: "/repo/wt"}
	m := NewTerminalModel()
	m.worktree = wt
	wtID := string(wt.ID())
	m.tabsByWorktree[wtID] = []*TerminalTab{
		{
			ID:    "test-tab",
			Name:  "Terminal 1",
			State: &TerminalState{VTerm: vterm.New(4, 3)},
		},
	}
	m.activeTabByWorktree[wtID] = 0
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
