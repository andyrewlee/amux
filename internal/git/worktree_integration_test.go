package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCreateDiscoverRemoveWorktree(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)
	project := data.NewProject(repo)

	worktreePath := filepath.Join(t.TempDir(), "feature-wt")

	if err := CreateWorktree(repo, worktreePath, "feature-wt", "HEAD"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("expected worktree path to exist: %v", err)
	}

	worktrees, err := DiscoverWorktrees(project)
	if err != nil {
		t.Fatalf("DiscoverWorktrees() error = %v", err)
	}
	if len(worktrees) < 2 {
		t.Fatalf("expected at least 2 worktrees, got %d", len(worktrees))
	}

	found := false
	for _, wt := range worktrees {
		wtRoot := wt.Root
		if eval, err := filepath.EvalSymlinks(wtRoot); err == nil {
			wtRoot = eval
		}
		expected := worktreePath
		if eval, err := filepath.EvalSymlinks(worktreePath); err == nil {
			expected = eval
		}
		if wtRoot == expected {
			found = true
			if wt.Branch != "feature-wt" {
				t.Fatalf("expected branch feature-wt, got %s", wt.Branch)
			}
		}
	}
	if !found {
		t.Fatalf("expected to find new worktree in DiscoverWorktrees")
	}

	if err := RemoveWorktree(repo, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree path to be removed, err=%v", err)
	}
}
