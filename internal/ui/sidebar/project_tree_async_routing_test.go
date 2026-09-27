package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestProjectTreeAsyncRoutingChangesTabStillApplies verifies a directory-load
// result reaches the project tree through TabbedSidebar even while the
// Changes tab is active — results are background completions, not tab input.
func TestProjectTreeAsyncRoutingChangesTabStillApplies(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ts := NewTabbedSidebar()
	ts.SetSize(40, 20)

	// SetWorkspace batches the Changes refresh with the tree's root load;
	// collect the produced messages and grab the tree's result.
	var loaded ProjectTreeDirectoryLoaded
	found := false
	for _, msg := range collectCmdMsgs(t, ts.SetWorkspace(wsFor(root))) {
		if l, ok := msg.(ProjectTreeDirectoryLoaded); ok {
			loaded, found = l, true
		}
	}
	if !found {
		t.Fatal("SetWorkspace batch produced no ProjectTreeDirectoryLoaded")
	}

	ts.activeTab = TabChanges // user switched away while the read was in flight
	if _, cmd := ts.Update(loaded); cmd != nil {
		pumpSidebar(t, ts, cmd)
	}
	if len(ts.projectTree.flatNodes) != 1 || ts.projectTree.flatNodes[0].Name != "visible.txt" {
		t.Fatalf("result lost on inactive tab: %+v", ts.projectTree.flatNodes)
	}
}

// TestProjectTreeAsyncRoutingBlurredTreeStillApplies verifies the tree applies
// results while unfocused — Update handles them before the focus guard.
func TestProjectTreeAsyncRoutingBlurredTreeStillApplies(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := NewProjectTree()
	m.SetSize(40, 20)
	if m.Focused() {
		t.Fatal("tree unexpectedly focused")
	}
	cmd := m.SetWorkspace(wsFor(root))
	res := <-runCmdAsync(cmd)
	if _, cmd := m.Update(res); cmd != nil {
		pumpTree(t, m, cmd)
	}
	if len(m.flatNodes) != 1 || m.flatNodes[0].Name != "a.txt" {
		t.Fatalf("blurred tree dropped its load result: %+v", m.flatNodes)
	}
}

// TestProjectTreeAsyncRoutingFollowupCmdsPropagate verifies apply-time
// follow-up commands (restored expansions) surface through the sidebar
// Update return value so the runtime can dispatch them.
func TestProjectTreeAsyncRoutingFollowupCmdsPropagate(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := NewProjectTree()
	m.SetSize(40, 20)
	pumpTree(t, m, m.SetWorkspace(wsFor(root)))
	pumpTree(t, m, m.expandNode(m.flatNodes[0])) // sub expanded

	// Refresh: the root result must return a follow-up command that restores
	// the remembered expansion — if it were dropped, sub would stay closed.
	_, cmd := m.Update(m.reloadTree()())
	if cmd == nil {
		t.Fatal("root apply returned no follow-up cmd for remembered expansion")
	}
	pumpTree(t, m, cmd)
	if len(m.flatNodes) != 2 || !m.flatNodes[0].Expanded || m.flatNodes[1].Name != "deep.txt" {
		t.Fatalf("restored expansion missing after refresh: %+v", m.flatNodes)
	}
}

// collectCmdMsgs runs a (possibly batched) command and returns every leaf
// message it produced — the changes fetch and the tree load ride together.
func collectCmdMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectCmdMsgs(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// pumpSidebar feeds a command's messages back through the tabbed sidebar
// until quiescent — the routing-aware variant of pumpTree.
func pumpSidebar(t *testing.T, ts *TabbedSidebar, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; steps < 64; steps++ {
		if len(queue) == 0 {
			return
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if _, follow := ts.Update(msg); follow != nil {
			queue = append(queue, follow)
		}
	}
	t.Fatal("pumpSidebar did not reach quiescence in 64 steps")
}
