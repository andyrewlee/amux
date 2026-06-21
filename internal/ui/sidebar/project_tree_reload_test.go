package sidebar

import "testing"

func TestProjectTreeReloadPreservesExpansionAndCursor(t *testing.T) {
	m := newSeededProjectTree(t)

	alpha := m.flatNodes[0]
	if alpha.Name != "alpha" {
		t.Fatalf("expected first node alpha, got %q", alpha.Name)
	}
	m.expandNode(alpha)
	m.rebuildFlatList()

	nestedPath := ""
	for i, node := range m.flatNodes {
		if node.Name == "nested.txt" {
			m.cursor = i
			nestedPath = node.Path
			break
		}
	}
	if nestedPath == "" {
		t.Fatal("expected nested.txt visible after expanding alpha")
	}

	m.reloadTree()

	stillVisible := false
	for _, node := range m.flatNodes {
		if node.Path == nestedPath {
			stillVisible = true
			break
		}
	}
	if !stillVisible {
		t.Fatal("reloadTree collapsed the previously expanded directory")
	}
	if m.cursor < 0 || m.cursor >= len(m.flatNodes) || m.flatNodes[m.cursor].Path != nestedPath {
		t.Fatalf("reloadTree did not preserve cursor position (cursor=%d)", m.cursor)
	}
}
