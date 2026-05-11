package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceRejectsStaleCleanupForManagedProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, gotWorkspacePath string) error {
			if repoPath != projectPath {
				t.Fatalf("repoPath = %q, want %q", repoPath, projectPath)
			}
			if gotWorkspacePath != workspacePath {
				t.Fatalf("workspacePath = %q, want %q", gotWorkspacePath, workspacePath)
			}
			return git.ErrUnregisteredWorkspacePath
		},
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(failed.Err, git.ErrUnregisteredWorkspacePath) {
		t.Fatalf("expected unregistered workspace error, got %v", failed.Err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected managed project root to remain on disk, err=%v", err)
	}
}
