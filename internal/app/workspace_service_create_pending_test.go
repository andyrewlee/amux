package app

import (
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestCreateWorkspaceInvalidNameReturnsPendingWorkspace(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := data.NewProject(repo)

	msg := service.CreateWorkspace(project, "bad/name", "main")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatalf("expected pending workspace on invalid-name failure")
	}
	if failed.Workspace.Name != "bad/name" {
		t.Fatalf("workspace name = %q, want %q", failed.Workspace.Name, "bad/name")
	}
}

func TestCreateWorkspaceInvalidBaseReturnsPendingWorkspace(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := data.NewProject(repo)

	msg := service.CreateWorkspace(project, "feature", "bad ref")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatalf("expected pending workspace on invalid-base failure")
	}
	if failed.Workspace.Name != "feature" {
		t.Fatalf("workspace name = %q, want %q", failed.Workspace.Name, "feature")
	}
}

func TestCreateWorkspaceInvalidProjectScopeReturnsPendingWorkspace(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	service := newWorkspaceService(nil, store, nil, workspacesRoot)
	project := &data.Project{
		Name: "../unsafe",
		Path: repo,
	}

	msg := service.CreateWorkspace(project, "feature", "main")()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatalf("expected pending workspace on invalid-scope failure")
	}
}
