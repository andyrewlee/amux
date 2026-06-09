package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestHandleWorkspaceDeletedClearsDirtyWorkspaceMarker(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		dashboard:            dashboard.New(),
		center:               center.New(nil),
		sidebar:              sidebar.NewTabbedSidebar(),
		sidebarTerminal:      sidebar.NewTerminalModel(),
		dirtyWorkspaces:      map[string]bool{wsID: true},
		deletingWorkspaceIDs: map[string]bool{wsID: true},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete success")
	}
	if app.dirtyWorkspaces[wsID] {
		t.Fatal("expected dirty workspace marker to be cleared on delete success")
	}
}

func TestSyncActiveWorkspacesToDashboard_SkipsDeleteInFlight(t *testing.T) {
	wsA := &data.Workspace{Repo: "/repo", Root: "/repo/a"}
	wsB := &data.Workspace{Repo: "/repo", Root: "/repo/b"}
	idA, idB := string(wsA.ID()), string(wsB.ID())

	app := &App{
		tmuxActivitySettled:    true,
		tmuxActiveWorkspaceIDs: map[string]bool{idA: true, idB: true},
		dashboard:              dashboard.New(),
	}
	app.markWorkspaceDeleteInFlight(wsA, true)
	app.syncActiveWorkspacesToDashboard()

	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 1 {
		t.Fatalf("expected 1 active workspace (delete-in-flight wsA excluded), got %d", got)
	}
}
