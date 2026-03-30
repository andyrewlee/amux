package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

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
