package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestTerminalResizesOnKeymapHintToggle(t *testing.T) {
	wt := &data.Worktree{Repo: "/repo", Root: "/repo/wt"}
	m := NewTerminalModel()
	m.worktree = wt
	wtID := string(wt.ID())
	m.tabsByWorktree[wtID] = []*TerminalTab{
		{
			ID:    "tab-1",
			Name:  "Terminal 1",
			State: &TerminalState{VTerm: vterm.New(10, 5)},
		},
	}
	m.activeTabByWorktree[wtID] = 0

	m.SetSize(80, 20)
	ts := m.getTerminal()
	if ts == nil {
		t.Fatalf("expected terminal state")
	}
	ts.mu.Lock()
	baseHeight := ts.lastHeight
	ts.mu.Unlock()
	if baseHeight <= 0 {
		t.Fatalf("expected base height > 0, got %d", baseHeight)
	}

	m.SetShowKeymapHints(true)
	ts.mu.Lock()
	helpHeight := ts.lastHeight
	ts.mu.Unlock()
	if helpHeight >= baseHeight {
		t.Fatalf("expected height to shrink with hints (base=%d help=%d)", baseHeight, helpHeight)
	}

	m.SetShowKeymapHints(false)
	ts.mu.Lock()
	restored := ts.lastHeight
	ts.mu.Unlock()
	if restored != baseHeight {
		t.Fatalf("expected height to restore after hints off (base=%d restored=%d)", baseHeight, restored)
	}
}
