package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestHandleTabDetached_PersistsSourceWorkspace(t *testing.T) {
	active := data.NewWorkspace("active", "main", "main", "/repo", "/repo")
	activeID := string(active.ID())
	sourceWorkspaceID := "ws-source"

	app := &App{
		activeWorkspace: active,
		dirtyWorkspaces: map[string]bool{},
	}

	cmd := app.handleTabDetached(messages.TabDetached{
		WorkspaceID: sourceWorkspaceID,
		Index:       3,
	})
	if cmd == nil {
		t.Fatal("expected non-nil persist cmd")
	}
	if !app.dirtyWorkspaces[sourceWorkspaceID] {
		t.Fatalf("expected source workspace %q to be marked dirty", sourceWorkspaceID)
	}
	if app.dirtyWorkspaces[activeID] {
		t.Fatalf("did not expect active workspace %q to be marked dirty", activeID)
	}
}

func TestHandleTabDetached_FallsBackToActiveWorkspace(t *testing.T) {
	active := data.NewWorkspace("active", "main", "main", "/repo", "/repo")
	activeID := string(active.ID())

	app := &App{
		activeWorkspace: active,
		dirtyWorkspaces: map[string]bool{},
	}

	cmd := app.handleTabDetached(messages.TabDetached{Index: 1})
	if cmd == nil {
		t.Fatal("expected non-nil persist cmd")
	}
	if !app.dirtyWorkspaces[activeID] {
		t.Fatalf("expected active workspace %q to be marked dirty", activeID)
	}
}
