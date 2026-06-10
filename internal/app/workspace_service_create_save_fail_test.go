package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestCreateWorkspace_SaveFailureRollsBackAndFails proves a post-worktree
// metadata-save failure surfaces as WorkspaceCreateFailed — never
// WorkspaceCreated, which would enqueue setup scripts and a projects reload
// for a workspace that no longer exists — and that the worktree and branch
// created earlier are rolled back.
func TestCreateWorkspace_SaveFailureRollsBackAndFails(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")

	saveErr := errors.New("metadata save boom")
	store := &failingDeleteStore{saveErr: saveErr}

	var removedPath, deletedBranch string
	mock := &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			// Simulate a successful worktree add so the create path reaches Save.
			return os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755)
		},
		removeWorkspace: func(repoPath, workspacePath string) error {
			removedPath = workspacePath
			return nil
		},
		deleteBranch: func(repoPath, branch string) error {
			deletedBranch = branch
			return nil
		},
	}

	svc := newWorkspaceService(nil, store, nil, workspacesRoot)
	svc.gitOps = mock

	project := data.NewProject(projectPath)
	msg := svc.CreateWorkspace(project, "feature", "main")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if !errors.Is(failed.Err, saveErr) {
		t.Fatalf("expected save error, got %v", failed.Err)
	}
	if failed.Workspace == nil || failed.Workspace.Name != "feature" {
		t.Fatalf("expected pending workspace in failure message, got %+v", failed.Workspace)
	}
	if removedPath != failed.Workspace.Root {
		t.Fatalf("expected rollback to remove worktree %s, removed %q", failed.Workspace.Root, removedPath)
	}
	if deletedBranch != "feature" {
		t.Fatalf("expected rollback to delete branch feature, deleted %q", deletedBranch)
	}
}
