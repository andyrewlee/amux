package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDeleteWorkspaceAllowsLegacyAliasRootWhenDiscovered(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)

	project := &data.Project{Name: "canonical", Path: "/repos/canonical/repo"}
	legacyRoot := filepath.Join(workspacesRoot, "legacy-alias", "feature")
	if err := os.MkdirAll(legacyRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(legacyRoot): %v", err)
	}
	workspace := data.NewWorkspace("feature", "feature", "main", project.Path, legacyRoot)

	origDiscover := discoverWorkspacesFn
	origRemove := removeWorkspaceFn
	origDeleteBranch := deleteBranchFn
	t.Cleanup(func() {
		discoverWorkspacesFn = origDiscover
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDeleteBranch
	})

	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return []data.Workspace{
			{Repo: project.Path, Root: legacyRoot},
		}, nil
	}

	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = func(repoPath, branch string) error { return nil }

	msg := service.DeleteWorkspace(project, workspace)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatalf("expected removeWorkspaceFn to run for discovered legacy alias root")
	}
}

func TestDeleteWorkspaceRejectsLegacyAliasRootWhenNotDiscovered(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)

	project := &data.Project{Name: "canonical", Path: "/repos/canonical/repo"}
	legacyRoot := filepath.Join(workspacesRoot, "legacy-alias", "feature")
	if err := os.MkdirAll(legacyRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(legacyRoot): %v", err)
	}
	workspace := data.NewWorkspace("feature", "feature", "main", project.Path, legacyRoot)

	origDiscover := discoverWorkspacesFn
	origRemove := removeWorkspaceFn
	t.Cleanup(func() {
		discoverWorkspacesFn = origDiscover
		removeWorkspaceFn = origRemove
	})

	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil
	}

	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := service.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected scope validation error when legacy alias root is not discoverable")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run when legacy alias root is not discoverable")
	}
}

func TestDeleteWorkspaceAllowsLegacyRelativeRepoWhenDiscovered(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)

	project := &data.Project{Name: "canonical", Path: filepath.Join(tmp, "repos", "canonical")}
	workspaceRoot := filepath.Join(workspacesRoot, project.Name, "feature")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceRoot): %v", err)
	}
	workspace := data.NewWorkspace("feature", "feature", "main", project.Name, workspaceRoot)

	origDiscover := discoverWorkspacesFn
	origRemove := removeWorkspaceFn
	origDeleteBranch := deleteBranchFn
	t.Cleanup(func() {
		discoverWorkspacesFn = origDiscover
		removeWorkspaceFn = origRemove
		deleteBranchFn = origDeleteBranch
	})

	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return []data.Workspace{
			{Repo: project.Path, Root: workspaceRoot},
		}, nil
	}

	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}
	deleteBranchFn = func(repoPath, branch string) error { return nil }

	msg := service.DeleteWorkspace(project, workspace)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatalf("expected removeWorkspaceFn to run for discovered relative-repo workspace")
	}
}

func TestDeleteWorkspaceRejectsLegacyRelativeRepoWhenNotDiscovered(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)

	project := &data.Project{Name: "canonical", Path: filepath.Join(tmp, "repos", "canonical")}
	workspaceRoot := filepath.Join(workspacesRoot, project.Name, "feature")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceRoot): %v", err)
	}
	workspace := data.NewWorkspace("feature", "feature", "main", project.Name, workspaceRoot)

	origDiscover := discoverWorkspacesFn
	origRemove := removeWorkspaceFn
	t.Cleanup(func() {
		discoverWorkspacesFn = origDiscover
		removeWorkspaceFn = origRemove
	})

	discoverWorkspacesFn = func(project *data.Project) ([]data.Workspace, error) {
		return nil, nil
	}

	removeCalled := false
	removeWorkspaceFn = func(repoPath, workspacePath string) error {
		removeCalled = true
		return nil
	}

	msg := service.DeleteWorkspace(project, workspace)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatalf("expected repo mismatch validation error")
	}
	if removeCalled {
		t.Fatalf("expected removeWorkspaceFn not to run for undiscovered relative-repo workspace")
	}
}
