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
	svc.killWorkspaceSessions = func(wsID string) error {
		order++
		killOrder = order
		return nil
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

func TestDeleteWorkspace_KillsPersistedLegacySessionNames(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitOps = &mockGitOps{}
	var killed []string
	svc.killWorkspaceSessionNames = func(names []string) error {
		killed = append(killed, names...)
		return nil
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)
	ws.OpenTabs = []data.TabInfo{
		{SessionName: "amux-legacy-workspace-tab-a"},
		{SessionName: "amux-legacy-workspace-tab-a"},
		{SessionName: "amux-legacy-workspace-tab-b"},
	}

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if len(killed) != 2 || killed[0] != "amux-legacy-workspace-tab-a" || killed[1] != "amux-legacy-workspace-tab-b" {
		t.Fatalf("killed persisted sessions = %v", killed)
	}
}

func TestDeleteWorkspace_RetainsMetadataWhenSessionCleanupFails(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "metadata"))
	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	branchDeleted := false
	svc := newWorkspaceService(nil, store, nil, workspacesRoot)
	svc.gitOps = &mockGitOps{deleteBranch: func(string, string) error {
		branchDeleted = true
		return nil
	}}
	svc.killWorkspaceSessions = func(string) error { return errors.New("tmux busy") }

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleteFailed); !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("metadata must remain for cleanup retry: %v", err)
	}
	if !store.IsDeleting(ws.ID()) {
		t.Fatal("delete tombstone must remain for startup recovery")
	}
	if branchDeleted {
		t.Fatal("branch deletion must wait until required session cleanup succeeds")
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
	svc.killWorkspaceSessions = func(wsID string) error {
		t.Fatal("failed delete must not kill workspace tmux sessions")
		return nil
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
	svc.killWorkspaceSessions = func(wsID string) error {
		killed = true
		return nil
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !killed {
		t.Fatal("expected sessions killed when removal failed after deleting workspace path")
	}
}
