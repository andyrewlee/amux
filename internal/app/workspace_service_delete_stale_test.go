package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceFallsBackForExternallyRemovedUnregisteredWorktree(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)

	project := &data.Project{Name: "repo", Path: "/repos/a/repo"}
	workspaceRoot := filepath.Join(service.primaryManagedProjectRoot(project), "feature")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceRoot): %v", err)
	}
	workspace := data.NewWorkspace("feature", "feature", "main", project.Path, workspaceRoot)

	origRemove := removeWorkspaceFn
	origDelete := deleteBranchFn
	t.Cleanup(func() {
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDelete
	})

	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		return errors.New("refusing fallback cleanup for unmanaged workspace path")
	}
	deleteBranchFn = func(repoPath, branch string) error { return nil }

	msg := service.DeleteWorkspace(project, workspace)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("expected stale workspace path to be cleaned up, err=%v", err)
	}
}
