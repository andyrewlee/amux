package sidebar

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func treeKey(m *ProjectTree, code rune) *ProjectTree {
	m2, _ := m.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	return m2
}

func treeNodeNames(m *ProjectTree) []string {
	var names []string
	for _, n := range m.flatNodes {
		names = append(names, n.Name)
	}
	return names
}

// flatNodes of the seeded tree is [alpha, beta, one.txt, two.txt] — dirs
// first, collapsed by default, children of collapsed dirs not yet loaded.
func TestProjectTreeSlashEntersFilterMode(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	if !m.filter.Active() {
		t.Fatal("/ should enter filter-input mode")
	}
	if !m.FilterActive() {
		t.Fatal("FilterActive should report input mode for the tabs digit guard")
	}
}

func TestProjectTreeFilterNarrowsFlatNodes(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	m = treeKey(m, 'b')
	m = treeKey(m, 'e')

	got := treeNodeNames(m)
	if len(got) != 1 || got[0] != "beta" {
		t.Fatalf("filtered nodes = %v, want [beta]", got)
	}
}

func TestProjectTreeFilterShowsCollapsedAncestors(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	// Expand alpha so nested.txt loads, then collapse it — the match must
	// surface through the collapsed ancestor (Expanded flag untouched).
	for i, n := range m.flatNodes {
		if n.Name == "alpha" {
			m.cursor = i
		}
	}
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	pumpTree(t, m, cmd)
	m, _ = m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})

	alpha := m.flatNodes[0]
	if alpha.Expanded {
		t.Fatal("precondition: alpha should be collapsed")
	}

	m = treeKey(m, '/')
	for _, r := range "nested" {
		m = treeKey(m, r)
	}

	got := treeNodeNames(m)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "nested.txt" {
		t.Fatalf("filtered nodes = %v, want [alpha nested.txt]", got)
	}
	// The ancestor's expansion flag is untouched — clearing the filter
	// restores the exact prior expansion state.
	if alpha.Expanded {
		t.Fatal("filter visibility must not mutate Expanded")
	}
}

func TestProjectTreeFilterEnterKeepsQueryApplied(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	m = treeKey(m, 'b')
	m = treeKey(m, 'e')
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m2
	if m.filter.Active() {
		t.Fatal("enter should exit filter-input mode")
	}
	if m.filter.Query() != "be" {
		t.Fatalf("query = %q after enter, want be", m.filter.Query())
	}
	if got := treeNodeNames(m); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("nodes after enter = %v, want [beta]", got)
	}
}

func TestProjectTreeFilterEscDismissesAppliedQuery(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	m = treeKey(m, 'b')
	m = treeKey(m, 'e')
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m2
	if got := treeNodeNames(m); len(got) != 1 {
		t.Fatalf("precondition: want [beta], got %v", got)
	}

	m2, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = m2
	if m.filter.Query() != "" {
		t.Fatalf("esc should clear the applied query, got %q", m.filter.Query())
	}
	if got := treeNodeNames(m); len(got) != 4 {
		t.Fatalf("esc should restore all nodes, got %v", got)
	}
}

func TestProjectTreeFilterEscWhileTypingClearsImmediately(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	m = treeKey(m, 'b')
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = m2
	if m.filter.Active() || m.filter.Query() != "" {
		t.Fatal("esc while typing should clear query and exit")
	}
	if got := treeNodeNames(m); len(got) != 4 {
		t.Fatalf("esc should restore all nodes, got %v", got)
	}
}

func TestProjectTreeFilterDigitsCapturedByWidget(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	// '1'/'2' are sidebar tab keys; while the tree filter is active they
	// must type into the input (the tabs.go guard covers the routing, the
	// widget's contract makes them text).
	m = treeKey(m, '2')
	if m.filter.Query() != "2" {
		t.Fatalf("Query() = %q, want %q — digit must type into the filter", m.filter.Query(), "2")
	}
}

func TestProjectTreeFilterNoMatchesRendersHint(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m = treeKey(m, '/')
	for _, r := range "zzzz" {
		m = treeKey(m, r)
	}
	if len(m.flatNodes) != 0 {
		t.Fatalf("expected zero matches, got %v", treeNodeNames(m))
	}
	view := m.View()
	if got := view; !containsAll(got, "no matching files") {
		t.Fatalf("empty-match view should render the hint, got:\n%s", view)
	}
}

func containsAll(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestProjectTreeFilterClearedOnWorkspaceSwitch(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m = treeKey(m, '/')
	m = treeKey(m, 'b')

	// A different workspace clears the filter — the Changes SetWorkspace
	// contract, so a phantom filter cannot capture keys under the new ws.
	other := newSeededProjectTree(t)
	pumpTree(t, m, m.SetWorkspace(other.workspace))
	if m.filter.Active() || m.filter.Query() != "" {
		t.Fatal("workspace switch should clear filter mode and query")
	}
}

func TestProjectTreeFilterRestorePreservesExpansion(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	// Expand alpha, filter on beta (hides alpha entirely), then clear —
	// alpha must still be expanded, proving the filter never touched
	// expansion state.
	for i, n := range m.flatNodes {
		if n.Name == "alpha" {
			m.cursor = i
		}
	}
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	pumpTree(t, m, cmd)
	if !m.flatNodes[0].Expanded {
		t.Fatal("precondition: alpha expanded")
	}

	m = treeKey(m, '/')
	m = treeKey(m, 'b')
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = m2

	got := treeNodeNames(m)
	if len(got) != 5 || got[0] != "alpha" || got[1] != "nested.txt" {
		t.Fatalf("cleared filter should restore the expanded view, got %v", got)
	}
}
