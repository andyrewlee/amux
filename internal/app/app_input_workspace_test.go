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
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete success")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty workspace marker to be cleared on delete success")
	}
}

func TestSyncActiveWorkspacesToDashboard_SkipsDeleteInFlight(t *testing.T) {
	wsA := &data.Workspace{Repo: "/repo", Root: "/repo/a"}
	wsB := &data.Workspace{Repo: "/repo", Root: "/repo/b"}
	idA, idB := string(wsA.ID()), string(wsB.ID())

	app := &App{
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{idA: true, idB: true},
		},
		dashboard: dashboard.New(),
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
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{wsID: true},
		},
		tmuxAvailable: true,
		dashboard:     dashboard.New(),
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
	if !app.tmuxActivity.scanInFlight {
		t.Fatal("expected failed delete to request a fresh tmux activity scan")
	}
}

func TestHandleWorkspaceDeleted_ClearsActiveWorkspace(t *testing.T) {
	wsDel := data.NewWorkspace("del", "del", "main", "/repo", "/repo/del")
	wsKeep := data.NewWorkspace("keep", "keep", "main", "/repo", "/repo/keep")
	idDel, idKeep := string(wsDel.ID()), string(wsKeep.ID())

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		tmuxActivity: tmuxActivityState{
			settled:            true,
			activeWorkspaceIDs: map[string]bool{idDel: true, idKeep: true},
		},
		lifecycle: workspaceLifecycleState{
			phases: map[string]lifecyclePhase{idDel: lifecycleDeleting},
		},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: wsDel})

	if app.tmuxActivity.activeWorkspaceIDs[idDel] {
		t.Fatal("expected deleted workspace cleared from the active set")
	}
	if !app.tmuxActivity.activeWorkspaceIDs[idKeep] {
		t.Fatal("expected surviving workspace to remain in the active set")
	}
}

func TestHandleWorkspaceDeleted_WithMetadataErrorRemovesLoadedWorkspace(t *testing.T) {
	wsDel := data.NewWorkspace("del", "del", "main", "/repo", "/repo/del")
	wsKeep := data.NewWorkspace("keep", "keep", "main", "/repo", "/repo/keep")
	project := data.NewProject("/repo")
	project.Workspaces = []data.Workspace{*wsDel, *wsKeep}
	wsID := string(wsDel.ID())

	app := &App{
		projects:        []data.Project{*project},
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		activeWorkspace: wsDel,
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
		},
	}
	app.dashboard.SetProjects(app.projects)

	cmds := app.handleWorkspaceDeleted(messages.WorkspaceDeleted{
		Workspace: wsDel,
		Err:       errors.New("metadata delete failed"),
	})

	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected deleted workspace removed from loaded projects, got %+v", app.projects)
	}
	if got := app.projects[0].Workspaces[0].Root; got != wsKeep.Root {
		t.Fatalf("expected surviving workspace %q, got %q", wsKeep.Root, got)
	}
	if app.activeWorkspace != nil {
		t.Fatal("expected metadata-error delete to still navigate away from deleted workspace")
	}
	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker cleared")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty marker cleared")
	}
	if len(cmds) == 0 {
		t.Fatal("expected metadata error to be reported")
	}
}
