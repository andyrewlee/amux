package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

func setupChangesScrollModel() *Model {
	m := New()
	m.SetSize(80, 10)
	m.Focus()

	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: fmt.Sprintf("file-%02d.txt", i),
			Kind: git.ChangeModified,
		})
	}
	m.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	_ = m.View()
	return m
}

func setupProjectTreeScrollModel(t *testing.T) *ProjectTree {
	t.Helper()

	root := t.TempDir()
	for i := 0; i < 20; i++ {
		path := filepath.Join(root, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	tree := NewProjectTree()
	tree.SetSize(80, 10)
	tree.Focus()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))
	_ = tree.View()
	return tree
}

func setupNestedProjectTreeScrollModel(t *testing.T) *ProjectTree {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	for i := 0; i < 20; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	tree := NewProjectTree()
	tree.SetSize(80, 10)
	tree.Focus()
	tree.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))

	if len(tree.flatNodes) == 0 {
		t.Fatal("expected project tree to have visible nodes")
	}
	parent := tree.flatNodes[0]
	if !parent.IsDir {
		t.Fatalf("expected first node to be a directory, got %+v", parent)
	}

	tree.expandNode(parent)
	tree.rebuildFlatList()
	_ = tree.View()
	return tree
}

func setupTabbedChangesScrollModel() *TabbedSidebar {
	sidebar := NewTabbedSidebar()
	sidebar.SetSize(80, 10)
	sidebar.Focus()
	sidebar.SetActiveTab(TabChanges)

	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: fmt.Sprintf("file-%02d.txt", i),
			Kind: git.ChangeModified,
		})
	}
	sidebar.SetGitStatus(&git.StatusResult{
		Clean:    false,
		Unstaged: unstaged,
	})
	_ = sidebar.View()
	return sidebar
}

func setupTabbedProjectScrollModel(t *testing.T) *TabbedSidebar {
	t.Helper()

	sidebar := NewTabbedSidebar()
	sidebar.SetSize(80, 10)
	sidebar.Focus()
	sidebar.SetActiveTab(TabProject)

	root := t.TempDir()
	for i := 0; i < 20; i++ {
		path := filepath.Join(root, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	sidebar.SetWorkspace(data.NewWorkspace("feature", "feature", "main", root, root))
	_ = sidebar.View()
	return sidebar
}

func changesCursorVisible(m *Model) bool {
	visibleHeight := m.visibleHeight()
	return m.cursor >= m.scrollOffset && m.cursor < m.scrollOffset+visibleHeight
}

func treeCursorVisible(m *ProjectTree) bool {
	visibleHeight := m.visibleHeight()
	return m.cursor >= m.scrollOffset && m.cursor < m.scrollOffset+visibleHeight
}
