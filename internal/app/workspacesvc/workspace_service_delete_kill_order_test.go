package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/testutil"
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
	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error {
			order++
			removeOrder = order
			return nil
		},
	}

	svc := New(nil, nil, nil, workspacesRoot)
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

	svc := New(nil, nil, nil, workspacesRoot)
	svc.gitOps = &testutil.FakeGitOps{}
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
	svc := New(nil, store, nil, workspacesRoot)
	svc.gitOps = &testutil.FakeGitOps{DeleteBranchFunc: func(string, string) error {
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
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		return scripts.IsRunning(ws)
	}, "timed out waiting for setup script to be tracked")

	removeCalled := false
	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, gotWorkspacePath string) error {
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

	svc := New(nil, nil, scripts, workspacesRoot)
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

	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error {
			return errors.New("remove failed")
		},
	}

	svc := New(nil, nil, nil, workspacesRoot)
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

	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error {
			if err := os.RemoveAll(workspacePath); err != nil {
				t.Fatalf("RemoveAll(workspacePath) error = %v", err)
			}
			return errors.New("remove failed after path removal")
		},
	}

	svc := New(nil, nil, nil, workspacesRoot)
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
		t.Fatal("expected sessions killed when removal failed after mutating workspace path")
	}
}
