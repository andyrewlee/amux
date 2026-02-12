package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceLegacyRelativeRepoAllowedWhenDiscoverable(t *testing.T) {
	saveAndRestoreDeleteStubs(t)

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var removeCalledWith string
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalledWith = repoPath
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return []data.Workspace{
			{Name: "feature", Root: wsRoot, Repo: project.Path},
		}, nil
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", "repo", wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T: %+v", msg, msg)
	}
	if removeCalledWith != projectPath {
		t.Fatalf("removeWorkspaceFn called with repoPath %q, want %q", removeCalledWith, projectPath)
	}
}

func TestDeleteWorkspaceLegacyRelativeRepoRejectedWhenNotDiscoverable(t *testing.T) {
	saveAndRestoreDeleteStubs(t)

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil // NOT discoverable
	}

	project := data.NewProject(filepath.Join(tmp, "repo"))
	ws := data.NewWorkspace("feature", "feature", "main", "repo", wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected 'does not match' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceLegacyAliasRootRejectedWhenPathMissing(t *testing.T) {
	saveAndRestoreDeleteStubs(t)

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	// wsRoot does NOT exist on disk — no MkdirAll
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")

	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		// Would be discoverable if path existed.
		return []data.Workspace{
			{Name: "feature", Root: wsRoot, Repo: project.Path},
		}, nil
	}

	project := data.NewProject(filepath.Join(tmp, "repo"))
	ws := data.NewWorkspace("feature", "feature", "main", "repo", wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T: %+v", msg, msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceLegacyAbsoluteRepoMismatchAllowedWhenDiscoverable(t *testing.T) {
	saveAndRestoreDeleteStubs(t)

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return []data.Workspace{
			{Name: "feature", Root: wsRoot, Repo: project.Path},
		}, nil
	}

	projectPath := filepath.Join(tmp, "repo")
	project := data.NewProject(projectPath)
	// Absolute ws.Repo that doesn't match project path.
	ws := data.NewWorkspace("feature", "feature", "main", filepath.Join(tmp, "other-repo"), wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T: %+v", msg, msg)
	}
	if !removeCalled {
		t.Fatal("expected removeWorkspaceFn to be called")
	}
}
