package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// killRecordingTmuxOps records session-kill calls so a delete test can prove no
// session is destroyed before the delete is validated.
type killRecordingTmuxOps struct {
	stubTmuxOps
	killTagsCalls int
	killWsCalls   int
}

func (k *killRecordingTmuxOps) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	k.killTagsCalls++
	return false, nil
}

func (k *killRecordingTmuxOps) KillWorkspaceSessions(string, tmux.Options) error {
	k.killWsCalls++
	return nil
}

// TestHandleDeleteWorkspace_DoesNotKillSessionsUpFront pins the keystone fix:
// dispatching a delete must not kill the workspace's tmux sessions, because all
// real validation runs later in the async DeleteWorkspace cmd. A rejected or
// failed delete must therefore be a no-op, not destroy live agent sessions.
func TestHandleDeleteWorkspace_DoesNotKillSessionsUpFront(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	project := data.NewProject("/repo")

	ops := &killRecordingTmuxOps{}
	app := &App{
		dashboard:   dashboard.New(),
		tmuxService: ops,
		tmuxOptions: tmux.Options{},
	}

	cmds := app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})
	// Run any returned cmds so an async kill would be triggered if one existed.
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if ops.killTagsCalls != 0 || ops.killWsCalls != 0 {
		t.Fatalf("delete dispatch must not kill sessions before validation; KillSessionsMatchingTags=%d KillWorkspaceSessions=%d",
			ops.killTagsCalls, ops.killWsCalls)
	}
	if !app.isWorkspaceDeleteInFlight(string(ws.ID())) {
		t.Fatal("expected workspace marked delete-in-flight after dispatch")
	}
}

// TestDeleteWorkspace_NavigatesHomeOnlyOnConfirmedDelete proves goHome moved off
// the up-front path: dispatching the delete leaves the active workspace put, and
// only the confirmed WorkspaceDeleted sends the user home.
func TestDeleteWorkspace_NavigatesHomeOnlyOnConfirmedDelete(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	project := data.NewProject("/repo")

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		activeWorkspace: ws,
	}

	_ = app.deleteWorkspace(project, ws)
	if app.activeWorkspace == nil {
		t.Fatal("deleteWorkspace must not navigate home before the delete is confirmed")
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})
	if app.activeWorkspace != nil {
		t.Fatal("expected goHome (activeWorkspace cleared) once the delete is confirmed")
	}
}

// TestHandleWorkspaceDeleted_NoTrailingSessionKill proves the trailing tmux
// cleanup was removed: the validated delete path already tore the sessions down,
// and re-killing by tag after the delete-in-flight flag clears would, on a
// delete-then-recreate at the same project+name (same wsID, same session names),
// kill the brand-new agent session.
func TestHandleWorkspaceDeleted_NoTrailingSessionKill(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")

	ops := &killRecordingTmuxOps{}
	app := &App{
		dashboard:            dashboard.New(),
		center:               center.New(nil),
		sidebar:              sidebar.NewTabbedSidebar(),
		sidebarTerminal:      sidebar.NewTerminalModel(),
		tmuxService:          ops,
		tmuxOptions:          tmux.Options{},
		deletingWorkspaceIDs: map[string]bool{string(ws.ID()): true},
	}

	cmds := app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if ops.killTagsCalls != 0 || ops.killWsCalls != 0 {
		t.Fatalf("handleWorkspaceDeleted must not re-kill sessions after the trailing cleanup was removed; KillSessionsMatchingTags=%d KillWorkspaceSessions=%d",
			ops.killTagsCalls, ops.killWsCalls)
	}
}
