package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

func TestDeleteWorkspace_KillsSessionsAndTearsDownBeforeWorktreeRemoval(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	order := 0
	killOrder, teardownOrder, removeOrder := -1, -1, -1
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
	svc.teardownProcesses = func(root string) (process.TeardownResult, error) {
		order++
		teardownOrder = order
		if root != workspacePath {
			t.Fatalf("teardown root = %q, want %q", root, workspacePath)
		}
		return process.TeardownResult{}, nil
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if killOrder == -1 || teardownOrder == -1 || removeOrder == -1 {
		t.Fatalf("expected kill, teardown, and removal to all run (kill=%d teardown=%d remove=%d)", killOrder, teardownOrder, removeOrder)
	}
	if !(killOrder < teardownOrder && teardownOrder < removeOrder) {
		t.Fatalf("expected kill (%d) → teardown (%d) → worktree removal (%d)", killOrder, teardownOrder, removeOrder)
	}
}

func TestDeleteWorkspace_TeardownFailureAbortsDelete(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			t.Fatal("worktree removal must not run when process teardown fails")
			return nil
		},
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	svc.killWorkspaceSessions = func(wsID string) {}
	svc.teardownProcesses = func(root string) (process.TeardownResult, error) {
		return process.TeardownResult{}, errors.New("survivors remain")
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected the teardown error to be surfaced")
	}
}

func TestDeleteWorkspace_StopsScriptsBeforeWorktreeRemoval(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(filepath.Join(projectPath, ".amux"), 0o755); err != nil {
		t.Fatalf("MkdirAll(project .amux) error = %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	configPath := filepath.Join(projectPath, ".amux", "workspaces.json")
	if err := os.WriteFile(configPath, []byte(`{"setup-workspace":["sleep 5"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile(workspaces.json) error = %v", err)
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)
	scripts := process.NewScriptRunner(6200, 10)
	t.Cleanup(func() { _ = scripts.Stop(ws) })
	if err := scripts.TrustRepoScripts(projectPath); err != nil {
		t.Fatalf("TrustRepoScripts() error = %v", err)
	}

	setupDone := make(chan error, 1)
	go func() {
		setupDone <- scripts.RunSetup(ws)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !scripts.IsRunning(ws) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for setup script to be tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	removeCalled := false
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, gotWorkspacePath string) error {
			removeCalled = true
			if scripts.IsRunning(ws) {
				t.Fatal("worktree removal ran while workspace script was still tracked")
			}
			if gotWorkspacePath != workspacePath {
				t.Fatalf("RemoveWorkspace path = %q, want %q", gotWorkspacePath, workspacePath)
			}
			return nil
		},
	}

	svc := newWorkspaceService(nil, nil, scripts, workspacesRoot)
	svc.gitOps = mock

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatal("expected worktree removal after script stop")
	}

	select {
	case <-setupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("setup script did not exit after workspace delete stopped it")
	}
}

func TestDeleteWorkspace_KillsSessionsEvenWhenWorktreeRemovalFails(t *testing.T) {
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
	// Sessions die before worktree removal is attempted; a post-validation
	// failure cannot resurrect them. This is the deliberate trade-off that
	// keeps service stacks from running out of half-deleted worktrees.
	if !killed {
		t.Fatal("expected sessions killed before the failed worktree removal")
	}
}

func TestDeleteWorkspace_ValidationFailureDoesNotKillSessions(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	otherRepo := filepath.Join(tmp, "other-repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = &mockGitOps{}
	svc.killWorkspaceSessions = func(wsID string) {
		t.Fatal("a delete rejected by validation must not kill workspace tmux sessions")
	}
	svc.teardownProcesses = func(root string) (process.TeardownResult, error) {
		t.Fatal("a delete rejected by validation must not tear down processes")
		return process.TeardownResult{}, nil
	}

	project := data.NewProject(projectPath)
	// Workspace repo mismatching the project path fails validate_repo_match
	// before any destructive step.
	ws := data.NewWorkspace("feature", "feature", "main", otherRepo, workspacePath)

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
