package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/testutil"
)

// TestCreateWorkspaceBranchOwnership pins the result flag to the actual
// command path: `worktree add -b` creates the branch (true), the
// already-exists fallback attaches (false), and failures never claim
// ownership. State assertions use real refs, not just the returned flag.
func TestCreateWorkspaceBranchOwnership(t *testing.T) {
	repo := testutil.InitRepo(t)
	managed := t.TempDir()

	t.Run("new branch reports ownership", func(t *testing.T) {
		wsPath := filepath.Join(managed, "owned")
		created, err := CreateWorkspaceWithResult(repo, wsPath, "owned-branch", "HEAD")
		if err != nil {
			t.Fatalf("CreateWorkspaceWithResult() error = %v", err)
		}
		if !created {
			t.Fatal("worktree add -b path must report branchCreated=true")
		}
		testutil.RunGit(t, repo, "rev-parse", "--verify", "refs/heads/owned-branch")
		if _, err := os.Stat(filepath.Join(wsPath, ".git")); err != nil {
			t.Fatalf("worktree missing .git: %v", err)
		}
	})

	t.Run("reused branch reports no ownership", func(t *testing.T) {
		// Seed a branch the create will attach to.
		testutil.RunGit(t, repo, "branch", "reused-branch")
		wantOID := testutil.RunGit(t, repo, "rev-parse", "refs/heads/reused-branch")

		wsPath := filepath.Join(managed, "reused")
		created, err := CreateWorkspaceWithResult(repo, wsPath, "reused-branch", "HEAD")
		if err != nil {
			t.Fatalf("CreateWorkspaceWithResult() error = %v", err)
		}
		if created {
			t.Fatal("attaching to a pre-existing branch must report branchCreated=false")
		}
		if got := testutil.RunGit(t, repo, "rev-parse", "refs/heads/reused-branch"); got != wantOID {
			t.Fatalf("attach moved refs/heads/reused-branch: got %s, want %s", got, wantOID)
		}
	})

	t.Run("failed create reports no ownership", func(t *testing.T) {
		wsPath := filepath.Join(managed, "failed")
		created, err := CreateWorkspaceWithResult(repo, wsPath, "failed-branch", "no-such-base")
		if err == nil {
			t.Fatal("expected failure for a missing base")
		}
		if created {
			t.Fatal("error path must report branchCreated=false")
		}
	})

	t.Run("error text is preserved", func(t *testing.T) {
		wsPath := filepath.Join(managed, "failed-wrap")
		_, err := CreateWorkspaceWithResult(repo, wsPath, "wrap-branch", "no-such-base")
		if err == nil {
			t.Fatal("expected failure for a missing base")
		}
		if compatErr := CreateWorkspace(repo, wsPath, "wrap-branch", "no-such-base"); compatErr == nil {
			t.Fatal("CreateWorkspace wrapper must surface the same failure")
		} else if compatErr.Error() != err.Error() {
			t.Fatalf("wrapper rewrote the error: %q vs %q", compatErr, err)
		}
	})
}
