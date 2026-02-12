package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceLegacyPathIntegration(t *testing.T) {
	skipIfNoGit(t)

	// Save and restore package-level stubs — use real git functions.
	origCreate := createWorkspaceFn
	origRemove := removeWorkspaceFn
	origDelete := deleteBranchFn
	origDiscover := discoverWorkspacesFn
	t.Cleanup(func() {
		createWorkspaceFn = origCreate
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDelete
		discoverWorkspacesFn = origDiscover
	})
	removeWorkspaceFn = git.RemoveWorkspace
	deleteBranchFn = git.DeleteBranch
	discoverWorkspacesFn = git.DiscoverWorkspaces

	// Set up a real git repo.
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	wsRoot := filepath.Join(workspacesRoot, filepath.Base(repo), "feature")
	runGit(t, repo, "worktree", "add", "-b", "feature", wsRoot, "main")

	// Verify worktree directory exists.
	if _, err := os.Stat(wsRoot); err != nil {
		t.Fatalf("expected worktree at %s: %v", wsRoot, err)
	}

	project := data.NewProject(repo)
	// Simulate a relative repo (legacy metadata).
	ws := data.NewWorkspace("feature", "feature", "main", filepath.Base(repo), wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T: %+v", msg, msg)
	}

	// Verify directory is removed.
	if _, err := os.Stat(wsRoot); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, got err: %v", err)
	}
}
