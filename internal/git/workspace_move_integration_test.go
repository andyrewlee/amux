package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveWorktreeMovesRealWorktree(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)
	base := t.TempDir()

	from := filepath.Join(base, "wt")
	runGit(t, repo, "worktree", "add", "-b", "feature", "--", from, "HEAD")

	toDir := filepath.Join(base, "container")
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}
	to := filepath.Join(toDir, "wt")

	if err := MoveWorktree(repo, from, to); err != nil {
		t.Fatalf("MoveWorktree() error = %v", err)
	}

	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("old worktree path still exists (stat err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(to, ".git")); err != nil {
		t.Fatalf("moved worktree missing .git marker: %v", err)
	}

	listOut := runGit(t, repo, "worktree", "list", "--porcelain")
	if !strings.Contains(listOut, to) {
		t.Fatalf("git worktree list does not include the moved path:\n%s", listOut)
	}
	if strings.Contains(listOut, from) {
		t.Fatalf("git worktree list still includes the old path:\n%s", listOut)
	}

	// The moved worktree must remain usable: a commit lands on its branch.
	runGit(t, to, "commit", "--allow-empty", "-m", "moved")
	logOut := runGit(t, repo, "log", "-1", "--format=%s", "feature")
	if strings.TrimSpace(logOut) != "moved" {
		t.Fatalf("commit in moved worktree not on the branch: %q", logOut)
	}
}

func TestMoveWorktreeRejectsUnregisteredDirectory(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)
	base := t.TempDir()

	from := filepath.Join(base, "not-a-worktree")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	toDir := filepath.Join(base, "container")
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}

	err := MoveWorktree(repo, from, filepath.Join(toDir, "not-a-worktree"))
	if err == nil {
		t.Fatalf("expected moving an unregistered directory to fail")
	}
}
