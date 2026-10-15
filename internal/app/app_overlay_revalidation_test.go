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

// TestPresentWorkspaceDialog_LiveSubjectOnly pins the plan-094 structural
// contract: build runs only at drain time and always with the re-resolved
// workspace — never the captured message pointer — and the pending-create
// clear happens on present.
func TestPresentWorkspaceDialog_LiveSubjectOnly(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	ws := &data.Workspace{Name: "ws-live", Repo: "/repo", Root: "/repo/ws-live"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{project: &a.projects[0], name: "pending"}

	var ran bool
	var got *data.Workspace
	a.presentWorkspaceDialog(ws, func(live *data.Workspace, _ *data.Project) {
		ran = true
		got = live
	})
	if len(a.pendingOverlayOpens) != 1 {
		t.Fatalf("expected the open queued, got %d", len(a.pendingOverlayOpens))
	}
	// Replace the loaded copy so the live pointer differs from the captured
	// one — build must receive the re-resolved value.
	replacement := *ws
	replacement.Name = "ws-live-renamed"
	a.projects[0].Workspaces[0] = replacement
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if !ran {
		t.Fatal("build never ran for a still-loaded subject")
	}
	if got == ws || got.Name != "ws-live-renamed" {
		t.Fatalf("build received the captured pointer (%q), want the re-resolved live workspace", got.Name)
	}
	if a.pendingWorkspaceCreate.project != nil || a.pendingWorkspaceCreate.name != "" {
		t.Fatal("pending create handoff survived a presented dialog — clear must run on present")
	}
}

// TestPresentWorkspaceDialog_RemovedSubjectNeverBuilds: a subject removed
// while its open sits queued never reaches build, and the dropped open does
// not tear down an unrelated in-flight create handoff (clear is
// present-only).
func TestPresentWorkspaceDialog_RemovedSubjectNeverBuilds(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	ws := &data.Workspace{Name: "ws-gone", Repo: "/repo", Root: "/repo/ws-gone"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws}}}
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{project: &a.projects[0], name: "pending"}

	ran := false
	a.presentWorkspaceDialog(ws, func(*data.Workspace, *data.Project) { ran = true })
	a.projects = nil // removed while queued
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if ran {
		t.Fatal("build ran for a workspace removed while queued")
	}
	if a.pendingWorkspaceCreate.project == nil {
		t.Fatal("dropped stale open cleared an unrelated pending create handoff")
	}
}

// TestPresentBulkTargetsDialog_StaleSetNeverBuilds pins the same contract
// for the marked-set sibling: a fully-loaded set losing a member while
// queued invalidates the open — the typed count the user would confirm was
// computed from the request-time set.
func TestPresentBulkTargetsDialog_StaleSetNeverBuilds(t *testing.T) {
	a := arbiterTestApp()
	a.overlays.runOutput = visibleRunOutput()
	ws1 := &data.Workspace{Name: "ws-a", Repo: "/repo", Root: "/repo/ws-a"}
	ws2 := &data.Workspace{Name: "ws-b", Repo: "/repo", Root: "/repo/ws-b"}
	a.projects = []data.Project{{Path: "/repo", Workspaces: []data.Workspace{*ws1, *ws2}}}
	targets := []bulkTarget{
		{project: &a.projects[0], workspace: ws1, markID: string(ws1.MetadataID())},
		{project: &a.projects[0], workspace: ws2, markID: string(ws2.MetadataID())},
	}

	ran := false
	a.presentBulkTargetsDialog(targets, false, func([]bulkTarget) { ran = true })
	a.projects[0].Workspaces = a.projects[0].Workspaces[:1] // ws-b removed while queued
	a.overlays.runOutput.Hide()
	a.drainPendingOverlayOpens()

	if ran {
		t.Fatal("build ran for a bulk set whose membership drifted while queued")
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
