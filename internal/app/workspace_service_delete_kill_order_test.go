package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestDeleteWorkspace_KillsSessionsBeforeWorktreeRemoval proves the delete path
// tears down the workspace's tmux sessions before removing the worktree, so the
// agent process group (CWD = worktree root) is gone before its directory is.
func TestDeleteWorkspace_KillsSessionsBeforeWorktreeRemoval(t *testing.T) {
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
	if killOrder >= removeOrder {
		t.Fatalf("expected kill (order %d) before worktree removal (order %d)", killOrder, removeOrder)
	}
}
