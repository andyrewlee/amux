package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestWorkspaceStatusReadySupersededAtDrain: the status-ready open's token
// check ran at message time — a second status open bumping runOutputToken
// while it sat queued must keep it from stamping state or presenting.
func TestWorkspaceStatusReadySupersededAtDrain(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.env = common.NewEnvDialog(nil)
	a.overlays.env.Show()
	a.overlays.runOutputToken = 5
	ws := &data.Workspace{Name: "ws-status", Repo: "/repo", Root: "/repo/ws"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}

	a.handleWorkspaceStatusReady(workspaceStatusReadyMsg{token: 5, ws: ws})
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected the status open queued, got %d", len(a.pendingOverlayOpens))
	}

	// A second status open before the queue drains supersedes this one.
	a.overlays.runOutputToken = 6
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()

	if a.overlays.runOutput != nil {
		t.Fatal("superseded status open presented — drain-time token check must drop it")
	}
	if a.overlays.runOutputWorkspace != nil {
		t.Fatal("superseded open still stamped run-output workspace state")
	}
}

// TestWorkspaceStatusReadyRemovedWorkspaceDropped: the status open's subject
// left the loaded projects while queued — the dialog must not present a
// workspace that no longer exists.
func TestWorkspaceStatusReadyRemovedWorkspaceDropped(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.env = common.NewEnvDialog(nil)
	a.overlays.env.Show()
	a.overlays.runOutputToken = 9
	ws := &data.Workspace{Name: "ws-status", Repo: "/repo", Root: "/repo/ws"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}

	a.handleWorkspaceStatusReady(workspaceStatusReadyMsg{token: 9, ws: ws})
	a.projects = nil // workspace removed while the open sits queued
	a.overlays.env.Hide()
	a.drainPendingOverlayOpens()

	if a.overlays.runOutput != nil {
		t.Fatal("status dialog presented for a workspace that no longer exists")
	}
	if a.overlays.runOutputWorkspace != nil {
		t.Fatal("stale open still stamped run-output workspace state")
	}
}

// TestOverlayOpenDropsStaleWorkspaceSubject_SubjectDialogs applies the
// plan-086 re-resolve contract to the four workspace-subject dialogs that
// lacked it: a workspace removed while its open sits queued must not
// present (and must not stamp overlay/dlg state).
func TestOverlayOpenDropsStaleWorkspaceSubject_SubjectDialogs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(a *App, ws *data.Workspace)
		bound func(a *App) bool // did the stale open still land?
	}{
		{"scripts", func(a *App, ws *data.Workspace) {
			a.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})
		}, func(a *App) bool { return a.overlays.scripts != nil || a.overlays.scriptsWorkspace != nil }},
		{"env", func(a *App, ws *data.Workspace) {
			a.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
		}, func(a *App) bool { return a.overlays.env != nil || a.overlays.envWorkspace != nil }},
		{"rename", func(a *App, ws *data.Workspace) {
			a.handleShowRenameWorkspaceDialog(messages.ShowRenameWorkspaceDialog{Workspace: ws})
		}, func(a *App) bool { return a.dialog != nil || a.dlg.workspace != nil }},
		{"commit", func(a *App, ws *data.Workspace) {
			a.handleShowCommitWorkspaceDialog(messages.ShowCommitWorkspaceDialog{Workspace: ws})
		}, func(a *App) bool { return a.dialog != nil || a.dlg.workspace != nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := arbiterTestApp()
			a.overlays.runOutput = visibleRunOutput()
			ws := &data.Workspace{Name: "ws-gone", Repo: "/repo", Root: "/repo/ws-gone"}
			a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}

			tc.open(a, ws)
			if len(a.pendingOverlayOpens) != 1 {
				t.Fatalf("expected the open queued, got %d", len(a.pendingOverlayOpens))
			}
			a.projects = nil // removed while queued
			a.overlays.runOutput.Hide()
			a.drainPendingOverlayOpens()

			if tc.bound(a) {
				t.Fatal("stale open presented or bound state")
			}
		})
	}
}

// TestOverlayOpenDropsStaleWorkspaceSubject_ProjectDialogs: a project removed
// while its open sits queued must not host a create or present a remove
// confirmation — both are project-subject opens.
func TestOverlayOpenDropsStaleWorkspaceSubject_ProjectDialogs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(a *App, p *data.Project)
		bound func(a *App) bool
	}{
		{"create", func(a *App, p *data.Project) {
			a.handleShowCreateWorkspaceDialog(messages.ShowCreateWorkspaceDialog{Project: p})
		}, func(a *App) bool { return a.dialog != nil }},
		{"remove", func(a *App, p *data.Project) {
			a.handleShowRemoveProjectDialog(messages.ShowRemoveProjectDialog{Project: p})
		}, func(a *App) bool { return a.dialog != nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := arbiterTestApp()
			a.overlays.runOutput = visibleRunOutput()
			a.projects = []data.Project{{Path: "/repo", Name: "proj"}}

			tc.open(a, &a.projects[0])
			if len(a.pendingOverlayOpens) != 1 {
				t.Fatalf("expected the open queued, got %d", len(a.pendingOverlayOpens))
			}
			a.projects = nil // removed while queued
			a.overlays.runOutput.Hide()
			a.drainPendingOverlayOpens()

			if tc.bound(a) {
				t.Fatal("project dialog presented for a project that no longer exists")
			}
		})
	}
}

// TestProjectEnvReadySupersededAtDrain: a queued env open whose seq was
// bumped while queued must not stamp or present the superseded seed.
func TestProjectEnvReadySupersededAtDrain(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	a.overlays.projectEnvSeq = 3

	a.handleProjectEnvReady(projectEnvReadyMsg{seq: 3, repo: "/repo"})
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected the env open queued, got %d", len(a.pendingOverlayOpens))
	}

	a.overlays.projectEnvSeq = 4 // a later fetch supersedes this one
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if a.overlays.projectEnv != nil {
		t.Fatal("superseded env open presented — drain-time seq check must drop it")
	}
}
