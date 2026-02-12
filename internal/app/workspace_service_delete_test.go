package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// saveAndRestoreDeleteStubs saves the current package-level function vars and
// returns a cleanup function that restores them. Tests should call t.Cleanup
// with the returned function.
func saveAndRestoreDeleteStubs(t *testing.T) {
	t.Helper()
	origCreate := createWorkspaceFn
	origRemove := removeWorkspaceFn
	origDelete := deleteBranchFn
	origDiscover := discoverWorkspacesFn
	t.Cleanup(func() {
		createWorkspaceFn = origCreate
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDelete
		discoverWorkspacesFn = origDiscover
	})
}

func noopDelete(repoPath, branch string) error { return nil }

func TestDeleteWorkspaceRejectsMissingProjectPath(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete

	project := &data.Project{Name: "repo", Path: ""}
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "project path is empty") {
		t.Fatalf("expected 'project path is empty' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceRejectsMissingWorkspaceRepo(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete

	project := data.NewProject("/tmp/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "workspace repo is empty") {
		t.Fatalf("expected 'workspace repo is empty' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceRejectsRepoMismatch(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil // not discoverable
	}

	project := data.NewProject("/tmp/repo-a")
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo-b", "/tmp/workspaces/repo-a/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected 'does not match' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceRejectsPathOutsideManagedProjectRoot(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil // not discoverable
	}

	project := data.NewProject("/tmp/repo")
	// Repo matches but root is outside managed project root.
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/other/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "outside managed project root") {
		t.Fatalf("expected 'outside managed project root' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceRejectsUnsafeProjectNameSegment(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil
	}

	project := &data.Project{Name: "../unsafe", Path: "/tmp/repo"}
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/../unsafe/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceRejectsSameNameDifferentProjectScope(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil
	}

	// Two projects both named "repo" but different paths.
	project := data.NewProject("/tmp/repo-owner-a")
	project.Name = "repo"
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo-owner-b", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected repo mismatch error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}

func TestDeleteWorkspaceAllowsLegacyProjectRoot(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	wsRoot := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return []data.Workspace{
			{Name: "feature", Root: wsRoot, Repo: project.Path},
		}, nil
	}

	project := data.NewProject(filepath.Join(tmp, "repo"))
	// Relative/stale ws.Repo that doesn't match project.Path after normalization.
	ws := data.NewWorkspace("feature", "feature", "main", "repo", wsRoot)

	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T: %+v", msg, msg)
	}
	if !removeCalled {
		t.Fatal("expected removeWorkspaceFn to be called")
	}
}

func TestDeleteWorkspaceRejectsLegacyWhenNotDiscoverable(t *testing.T) {
	saveAndRestoreDeleteStubs(t)
	var removeCalled bool
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = noopDelete
	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil // NOT discoverable
	}

	project := data.NewProject("/tmp/repo")
	// Relative/stale ws.Repo that doesn't match.
	ws := data.NewWorkspace("feature", "feature", "main", "repo", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected repo mismatch error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspaceFn should not have been called")
	}
}
