package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceStaleCleanupOnRemoveFailure(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// No .git file — workspace is stale.

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			return errors.New("worktree not registered")
		},
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T: %+v", msg, msg)
	}
	if _, err := os.Stat(wsRoot); !os.IsNotExist(err) {
		t.Fatalf("expected workspace directory to be removed, but it still exists")
	}
}

func TestDeleteWorkspaceStaleCleanupRefusesWhenGitExists(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Create .git file — workspace is NOT stale.
	gitPath := filepath.Join(wsRoot, ".git")
	if err := os.WriteFile(gitPath, []byte("gitdir: ../../../.git/worktrees/feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			return errors.New("worktree remove failed")
		},
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T: %+v", msg, msg)
	}
	if failed.Err == nil {
		t.Fatal("expected joined error, got nil")
	}
	errMsg := failed.Err.Error()
	if !containsAll(errMsg, "worktree remove failed", "workspace still has git metadata") {
		t.Fatalf("expected joined error with both messages, got: %v", failed.Err)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
