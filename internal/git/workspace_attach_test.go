package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestAttachWorkspaceWorktree_AttachesKeptBranch is the restore primitive's
// contract: the worktree lands on the existing branch tip, and the branch
// ref itself is never moved or recreated.
func TestAttachWorkspaceWorktree_AttachesKeptBranch(t *testing.T) {
	repo := testutil.InitRepo(t)
	managed := t.TempDir()
	testutil.RunGit(t, repo, "checkout", "-b", "kept")
	testutil.RunGit(t, repo, "commit", "--allow-empty", "-m", "tip")
	wantOID := testutil.RunGit(t, repo, "rev-parse", "refs/heads/kept")
	testutil.RunGit(t, repo, "checkout", "main")

	wsPath := filepath.Join(managed, "restored")
	if err := AttachWorkspaceWorktree(repo, wsPath, "kept"); err != nil {
		t.Fatalf("AttachWorkspaceWorktree() error = %v", err)
	}
	if got := testutil.RunGit(t, wsPath, "rev-parse", "HEAD"); got != wantOID {
		t.Fatalf("worktree HEAD = %s, want kept tip %s", got, wantOID)
	}
	if got := testutil.RunGit(t, repo, "rev-parse", "refs/heads/kept"); got != wantOID {
		t.Fatalf("attach moved refs/heads/kept: %s -> %s", wantOID, got)
	}
}

// TestAttachWorkspaceWorktree_MissingBranchFailsHonestly — the kept branch
// deleted between shelve and restore must surface as an error, not recreate
// the branch from a fabricated base. The worktree dir must not be created.
func TestAttachWorkspaceWorktree_MissingBranchFailsHonestly(t *testing.T) {
	repo := testutil.InitRepo(t)
	managed := t.TempDir()

	wsPath := filepath.Join(managed, "restored")
	if err := AttachWorkspaceWorktree(repo, wsPath, "gone"); err == nil {
		t.Fatal("attach of a missing branch must fail")
	}
	if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
		t.Fatalf("failed attach left %s behind: stat err = %v", wsPath, err)
	}
	if out := testutil.RunGit(t, repo, "branch", "--list", "gone"); out != "" {
		t.Fatalf("attach recreated the missing branch: %q", out)
	}
}

// TestWorktreeIdentity reports the owning repo and checked-out branch for
// real worktrees — the two facts restore verifies before adopting a dir.
func TestWorktreeIdentity(t *testing.T) {
	repo := testutil.InitRepo(t)
	managed := t.TempDir()
	testutil.RunGit(t, repo, "branch", "feature")

	wsPath := filepath.Join(managed, "wt")
	if err := AttachWorkspaceWorktree(repo, wsPath, "feature"); err != nil {
		t.Fatalf("AttachWorkspaceWorktree() error = %v", err)
	}
	gotRepo, gotBranch, err := WorktreeIdentity(wsPath)
	if err != nil {
		t.Fatalf("WorktreeIdentity() error = %v", err)
	}
	if data.NormalizePath(gotRepo) != data.NormalizePath(repo) {
		t.Fatalf("identity repo = %q, want %q", gotRepo, repo)
	}
	if gotBranch != "feature" {
		t.Fatalf("identity branch = %q, want feature", gotBranch)
	}

	foreign := testutil.InitRepo(t)
	if gotRepo, _, err = WorktreeIdentity(foreign); err != nil {
		t.Fatalf("WorktreeIdentity(foreign) error = %v", err)
	}
	if data.NormalizePath(gotRepo) == data.NormalizePath(repo) {
		t.Fatalf("identity of a foreign repo returned our repo %q", gotRepo)
	}

	if _, _, err := WorktreeIdentity(t.TempDir()); err == nil {
		t.Fatal("identity of a non-worktree must fail")
	}
}
