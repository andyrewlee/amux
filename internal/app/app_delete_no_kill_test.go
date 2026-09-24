package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestHandleDeleteWorkspace_DoesNotKillSessionsUpFront pins the keystone fix:
// dispatching a delete must not kill the workspace's tmux sessions, because all
// real validation runs later in the async DeleteWorkspace cmd. A rejected or
// failed delete must therefore be a no-op, not destroy live agent sessions.
func TestHandleDeleteWorkspace_DoesNotKillSessionsUpFront(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	project := data.NewProject("/repo")

	ops := &tmuxops.FakeTmuxOps{}
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

	if len(ops.KillTagMatches()) != 0 || len(ops.KilledWorkspaceIDs()) != 0 {
		t.Fatalf("delete dispatch must not kill sessions before validation; KillSessionsMatchingTags=%d KillWorkspaceSessions=%d",
			len(ops.KillTagMatches()), len(ops.KilledWorkspaceIDs()))
	}
	if !app.isWorkspaceMutationInFlight(string(ws.ID())) {
		t.Fatal("expected workspace marked mutation-in-flight after dispatch")
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
// and re-killing by tag after the mutation-in-flight flag clears would, on a
// delete-then-recreate at the same project+name (same wsID, same session names),
// kill the brand-new agent session.
func TestHandleWorkspaceDeleted_NoTrailingSessionKill(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")

	ops := &tmuxops.FakeTmuxOps{}
	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		tmuxService:     ops,
		tmuxOptions:     tmux.Options{},
		lifecycle: workspaceLifecycleState{
			phases: map[string]lifecyclePhase{string(ws.ID()): lifecycleMutating},
		},
	}

	cmds := app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if len(ops.KillTagMatches()) != 0 || len(ops.KilledWorkspaceIDs()) != 0 {
		t.Fatalf("handleWorkspaceDeleted must not re-kill sessions after the trailing cleanup was removed; KillSessionsMatchingTags=%d KillWorkspaceSessions=%d",
			len(ops.KillTagMatches()), len(ops.KilledWorkspaceIDs()))
	}
}

// TestKillWorkspaceSessionsSync_AllInstances proves deletion tears down every
// session for the workspace. A worktree is host-global, so retaining a session
// merely because another amux instance created it leaves an agent running in a
// deleted directory.
func TestKillWorkspaceSessionsSync_AllInstances(t *testing.T) {
	t.Run("uses a workspace-wide tag match and legacy prefix kill", func(t *testing.T) {
		ops := &tmuxops.FakeTmuxOps{}
		app := &App{tmuxService: ops, instanceID: "inst-A"}

		if err := app.killWorkspaceSessionsSync("ws-1"); err != nil {
			t.Fatal(err)
		}

		if len(ops.KilledWorkspaceIDs()) != 1 {
			t.Fatalf("expected one workspace prefix cleanup, got %d", len(ops.KilledWorkspaceIDs()))
		}
		if len(ops.KilledMissingTag()) != 0 {
			t.Fatalf("expected no instance-filtered legacy cleanup, got %d", len(ops.KilledMissingTag()))
		}
		if _, ok := ops.LastKillTagMatch()["@amux_instance"]; ok {
			t.Fatalf("delete cleanup must not be instance-scoped, got tags %v", ops.LastKillTagMatch())
		}
		if ops.LastKillTagMatch()["@amux_workspace"] != "ws-1" {
			t.Fatalf("expected @amux_workspace tag, got %v", ops.LastKillTagMatch())
		}
	})

	t.Run("empty instance ID has the same workspace-wide behavior", func(t *testing.T) {
		ops := &tmuxops.FakeTmuxOps{}
		app := &App{tmuxService: ops, instanceID: ""}

		if err := app.killWorkspaceSessionsSync("ws-1"); err != nil {
			t.Fatal(err)
		}

		if _, ok := ops.LastKillTagMatch()["@amux_instance"]; ok {
			t.Fatalf("empty instanceID must not add @amux_instance, got %v", ops.LastKillTagMatch())
		}
		if len(ops.KilledWorkspaceIDs()) != 1 {
			t.Fatalf("expected broad workspace prefix cleanup, got %d", len(ops.KilledWorkspaceIDs()))
		}
		if len(ops.KilledMissingTag()) != 0 {
			t.Fatalf("empty instanceID should not use missing-tag fallback, got %d", len(ops.KilledMissingTag()))
		}
		if ops.LastKillTagMatch()["@amux_workspace"] != "ws-1" {
			t.Fatalf("expected @amux_workspace tag even when broad, got %v", ops.LastKillTagMatch())
		}
	})
}

func TestKillWorkspaceSessionNamesSync_OnlyKillsAmuxNamespace(t *testing.T) {
	ops := &tmuxops.FakeTmuxOps{}
	app := &App{tmuxService: ops}

	if err := app.killWorkspaceSessionNamesSync([]string{" amux-owned-agent ", "unrelated-session"}); err != nil {
		t.Fatal(err)
	}
	if len(ops.KilledSessions()) != 1 || ops.KilledSessions()[0] != "amux-owned-agent" {
		t.Fatalf("killed exact sessions = %v, want only amux-owned-agent", ops.KilledSessions())
	}
}
