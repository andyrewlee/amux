package app

import (
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleCreateWorkspaceSkipsPendingTrackingWithoutService(t *testing.T) {
	repo := t.TempDir()
	app := &App{
		dashboard:            dashboard.New(),
		creatingWorkspaceIDs: make(map[string]bool),
	}
	project := &data.Project{Name: "repo", Path: repo}

	_ = app.handleCreateWorkspace(messages.CreateWorkspace{
		Project: project,
		Name:    "feature",
		Base:    "main",
	})

	if len(app.creatingWorkspaceIDs) != 0 {
		t.Fatalf("expected no pending workspace IDs without workspace service, got %v", app.creatingWorkspaceIDs)
	}
}

func TestHandleCreateWorkspaceTracksAndClearsPendingIDOnFailure(t *testing.T) {
	repo := t.TempDir()
	tmp := t.TempDir()
	service := newWorkspaceService(
		nil,
		data.NewWorkspaceStore(filepath.Join(tmp, "metadata")),
		nil,
		filepath.Join(tmp, "workspaces"),
	)
	app := &App{
		workspaceService:       service,
		dashboard:              dashboard.New(),
		creatingWorkspaceIDs:   make(map[string]bool),
		tmuxActiveWorkspaceIDs: make(map[string]bool),
	}
	project := &data.Project{
		Name: "../unsafe",
		Path: repo,
	}

	cmds := app.handleCreateWorkspace(messages.CreateWorkspace{
		Project: project,
		Name:    "feature",
		Base:    "main",
	})

	var failed messages.WorkspaceCreateFailed
	foundFailure := false
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(messages.WorkspaceCreateFailed); ok {
			failed = msg
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Fatal("expected create workspace command to return WorkspaceCreateFailed")
	}
	if failed.Workspace == nil {
		t.Fatal("expected failure to include pending workspace")
	}
	id := string(failed.Workspace.ID())
	if !app.creatingWorkspaceIDs[id] {
		t.Fatalf("expected pending workspace ID %s to be tracked", id)
	}

	_ = app.handleWorkspaceCreateFailed(failed)
	if app.creatingWorkspaceIDs[id] {
		t.Fatalf("expected pending workspace ID %s to be cleared after failure", id)
	}
}
