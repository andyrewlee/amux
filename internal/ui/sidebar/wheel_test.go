package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/vterm"
)

// consumableChanges returns a *Model whose changes list has more than one
// selectable (non-header) entry, so canConsumeWheel reports true.
func consumableChanges(t *testing.T) *Model {
	t.Helper()
	m := New()
	m.SetSize(80, 20)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Unstaged: []git.Change{
			{Path: "a.go", Kind: git.ChangeModified},
			{Path: "b.go", Kind: git.ChangeModified},
		},
	})
	if !m.canConsumeWheel() {
		t.Fatal("setup: expected changes model to consume wheel")
	}
	return m
}

// consumableTree returns a *ProjectTree with more than one flat node, so
// canConsumeWheel reports true.
func consumableTree(t *testing.T) *ProjectTree {
	t.Helper()
	tree := NewProjectTree()
	tree.SetSize(80, 20)
	tree.workspace = data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	tree.flatNodes = []*ProjectTreeNode{
		{Name: "root", Path: "/tmp/repo/feature", IsDir: true},
		{Name: "main.go", Path: "/tmp/repo/feature/main.go", IsDir: false},
	}
	if !tree.canConsumeWheel() {
		t.Fatal("setup: expected project tree to consume wheel")
	}
	return tree
}

func TestChangesCanConsumeWheelWithShortSelectableList(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Unstaged: []git.Change{
			{Path: "a.go", Kind: git.ChangeModified},
			{Path: "b.go", Kind: git.ChangeModified},
		},
	})

	if !m.canConsumeWheel() {
		t.Fatal("expected short changes list with multiple files to consume wheel")
	}
}

func TestChangesCanConsumeWheelIgnoresHeaderOnlyOverflow(t *testing.T) {
	m := New()
	m.SetSize(80, 1)
	m.SetGitStatus(&git.StatusResult{
		Clean: false,
		Unstaged: []git.Change{
			{Path: "only.go", Kind: git.ChangeModified},
		},
	})

	if m.canConsumeWheel() {
		t.Fatal("expected single-file changes list to ignore header-only overflow")
	}
}

func TestProjectTreeCanConsumeWheelWithShortList(t *testing.T) {
	tree := NewProjectTree()
	tree.SetSize(80, 20)
	tree.workspace = data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	tree.flatNodes = []*ProjectTreeNode{
		{Name: "root", Path: "/tmp/repo/feature", IsDir: true},
		{Name: "main.go", Path: "/tmp/repo/feature/main.go", IsDir: false},
	}

	if !tree.canConsumeWheel() {
		t.Fatal("expected short project tree with multiple nodes to consume wheel")
	}
}

func TestProjectTreeCannotConsumeWheelWithSingleNode(t *testing.T) {
	tree := NewProjectTree()
	tree.SetSize(80, 1)
	tree.workspace = data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/repo/feature")
	tree.flatNodes = []*ProjectTreeNode{
		{Name: "root", Path: "/tmp/repo/feature", IsDir: true},
	}

	if tree.canConsumeWheel() {
		t.Fatal("expected single-node project tree not to consume wheel")
	}
}

func TestTabbedSidebarCanConsumeWheel(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *TabbedSidebar
		want  bool
	}{
		{
			name:  "nil receiver",
			build: func(t *testing.T) *TabbedSidebar { return nil },
			want:  false,
		},
		{
			name: "changes tab with consumable list",
			build: func(t *testing.T) *TabbedSidebar {
				s := NewTabbedSidebar()
				s.activeTab = TabChanges
				s.changes = consumableChanges(t)
				return s
			},
			want: true,
		},
		{
			name: "changes tab with empty list",
			build: func(t *testing.T) *TabbedSidebar {
				s := NewTabbedSidebar()
				s.activeTab = TabChanges
				// Fresh changes model: no git status, so nothing to scroll.
				return s
			},
			want: false,
		},
		{
			name: "project tab with consumable tree",
			build: func(t *testing.T) *TabbedSidebar {
				s := NewTabbedSidebar()
				s.activeTab = TabProject
				s.projectTree = consumableTree(t)
				return s
			},
			want: true,
		},
		{
			name: "project tab with single node",
			build: func(t *testing.T) *TabbedSidebar {
				s := NewTabbedSidebar()
				s.activeTab = TabProject
				// Fresh project tree: no workspace/nodes, nothing to scroll.
				return s
			},
			want: false,
		},
		{
			name: "unknown active tab falls through to false",
			build: func(t *testing.T) *TabbedSidebar {
				s := NewTabbedSidebar()
				// Force a tab value outside the known cases; even with a
				// consumable inner model, the switch default returns false.
				s.changes = consumableChanges(t)
				s.projectTree = consumableTree(t)
				s.activeTab = SidebarTab(99)
				return s
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.build(t)
			if got := s.CanConsumeWheel(); got != tt.want {
				t.Fatalf("CanConsumeWheel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// terminalModelWithTab returns a workspace-bound TerminalModel with a single
// active tab whose State is the given value, exercising TerminalModel.
// CanConsumeWheel against a fully in-memory tab (no live PTY/tmux).
func terminalModelWithTab(t *testing.T, state *TerminalState) *TerminalModel {
	t.Helper()
	m := NewTerminalModel()
	m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
	tab := &TerminalTab{ID: generateTerminalTabID(), Name: "Terminal 1", State: state}
	seedTabs(t, m, tab)
	return m
}

func TestTerminalModelCanConsumeWheel(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var m *TerminalModel
		if m.CanConsumeWheel() {
			t.Fatal("expected nil TerminalModel not to consume wheel")
		}
	})

	t.Run("no tabs", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		if m.CanConsumeWheel() {
			t.Fatal("expected empty terminal model not to consume wheel")
		}
	})

	t.Run("active index out of range", func(t *testing.T) {
		m := terminalModelWithTab(t, &TerminalState{VTerm: scrolledVTerm(t)})
		// Point the active index past the end of the tab slice.
		m.setActiveTabIdx(5)
		if m.CanConsumeWheel() {
			t.Fatal("expected out-of-range active index not to consume wheel")
		}
	})

	t.Run("negative active index", func(t *testing.T) {
		m := terminalModelWithTab(t, &TerminalState{VTerm: scrolledVTerm(t)})
		m.setActiveTabIdx(-1)
		if m.CanConsumeWheel() {
			t.Fatal("expected negative active index not to consume wheel")
		}
	})

	t.Run("nil active tab", func(t *testing.T) {
		m := NewTerminalModel()
		m.setWorkspace(&data.Workspace{Repo: "/repo", Root: "/repo/ws"})
		// Seed a single nil tab and mark it active.
		wsID := m.workspaceID()
		m.tabs.ByWorkspace[wsID] = []*TerminalTab{nil}
		m.tabs.ActiveByWorkspace[wsID] = 0
		if m.CanConsumeWheel() {
			t.Fatal("expected nil active tab not to consume wheel")
		}
	})

	t.Run("nil state", func(t *testing.T) {
		m := terminalModelWithTab(t, nil)
		if m.CanConsumeWheel() {
			t.Fatal("expected tab with nil state not to consume wheel")
		}
	})

	t.Run("nil vterm", func(t *testing.T) {
		m := terminalModelWithTab(t, &TerminalState{VTerm: nil})
		if m.CanConsumeWheel() {
			t.Fatal("expected nil VTerm not to consume wheel")
		}
	})

	t.Run("vterm without scrollback", func(t *testing.T) {
		// A fresh small VTerm with no scrollback reports MaxViewOffset()==0.
		vt := vterm.New(10, 3)
		if vt.MaxViewOffset() != 0 {
			t.Fatalf("setup: expected zero max view offset, got %d", vt.MaxViewOffset())
		}
		m := terminalModelWithTab(t, &TerminalState{VTerm: vt})
		if m.CanConsumeWheel() {
			t.Fatal("expected VTerm without scrollback not to consume wheel")
		}
	})

	t.Run("vterm with scrollback", func(t *testing.T) {
		vt := scrolledVTerm(t)
		if vt.MaxViewOffset() <= 0 {
			t.Fatalf("setup: expected positive max view offset, got %d", vt.MaxViewOffset())
		}
		m := terminalModelWithTab(t, &TerminalState{VTerm: vt})
		if !m.CanConsumeWheel() {
			t.Fatal("expected VTerm with scrollback to consume wheel")
		}
	})
}
