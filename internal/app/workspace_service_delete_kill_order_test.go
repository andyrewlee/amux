package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspace_KillsSessionsAfterWorktreeRemoval(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	order := 0
	killOrder, removeOrder := -1, -1
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			order++
			removeOrder = order
			return nil
		},
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	svc.killWorkspaceSessions = func(wsID string) {
		order++
		killOrder = order
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if killOrder == -1 {
		t.Fatal("expected workspace tmux sessions to be killed during delete")
	}
	if removeOrder == -1 {
		t.Fatal("expected the worktree to be removed during delete")
	}
	if killOrder <= removeOrder {
		t.Fatalf("expected kill (order %d) after worktree removal (order %d)", killOrder, removeOrder)
	}
}

func TestDeleteWorkspace_DoesNotKillSessionsWhenWorktreeRemovalFails(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			return errors.New("remove failed")
		},
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	svc.killWorkspaceSessions = func(wsID string) {
		t.Fatal("failed delete must not kill workspace tmux sessions")
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleteFailed); !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
}

func TestDeleteWorkspace_KillsSessionsWhenRemovalFailsAfterPathGone(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			if err := os.RemoveAll(workspacePath); err != nil {
				t.Fatalf("RemoveAll(workspacePath) error = %v", err)
			}
			return errors.New("remove failed after path removal")
		},
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	killed := false
	svc.killWorkspaceSessions = func(wsID string) {
		killed = true
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleteFailed); !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if !killed {
		t.Fatal("expected sessions killed when removal failed after deleting workspace path")
	}
}
