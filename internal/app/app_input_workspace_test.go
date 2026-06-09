package app

import (
	"errors"
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

func TestHandleWorkspaceDeleteFailedRequestsFreshActivityScan(t *testing.T) {
	ws := &data.Workspace{Repo: "/repo", Root: "/repo/a"}
	wsID := string(ws.ID())
	app := &App{
		tmuxActivitySettled:    true,
		tmuxActiveWorkspaceIDs: map[string]bool{wsID: true},
		tmuxAvailable:          true,
		dashboard:              dashboard.New(),
	}

	app.markWorkspaceDeleteInFlight(ws, true)
	app.syncActiveWorkspacesToDashboard()
	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 0 {
		t.Fatalf("expected active workspace to be filtered during delete, got %d", got)
	}

	app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{
		Workspace: ws,
		Err:       errors.New("delete failed"),
	})
	if got := dashboardActiveWorkspaceCount(app.dashboard); got != 0 {
		t.Fatalf("expected cached active state to stay filtered until fresh scan, got %d", got)
	}
	if !app.tmuxActivityScanInFlight {
		t.Fatal("expected failed delete to request a fresh tmux activity scan")
	}
}
