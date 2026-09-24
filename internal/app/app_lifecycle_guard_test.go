package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// lifecycleTestApp builds an App with the collaborators the lifecycle
// dispatch/result handlers touch. workspaceService is real (backed by a temp
// store) so loadProjects/runSetup emit observable commands.
func lifecycleTestApp(t *testing.T) *App {
	t.Helper()
	store := data.NewWorkspaceStore(t.TempDir())
	return &App{
		dashboard:        dashboard.New(),
		center:           center.New(nil),
		sidebar:          sidebar.NewTabbedSidebar(),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(&testutil.FakeProjectRegistry{}, store, nil, ""),
		lifecycle:        newWorkspaceLifecycleState(),
		tmuxActivity:     newTmuxActivityState(),
	}
}

func lifecycleTestWorkspace() (*data.Workspace, *data.Project) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	return ws, data.NewProject("/repo")
}

// TestLifecycleDispatchInFlightGuard pins the contract all three lifecycle
// mutations share: the first dispatch marks mutation-in-flight, and every
// second dispatch while that mark stands is rejected without reaching the
// service (the returned cmd list is the only path to the service call).
func TestLifecycleDispatchInFlightGuard(t *testing.T) {
	ws, project := lifecycleTestWorkspace()

	dispatches := map[string]func(a *App) int{
		"delete": func(a *App) int {
			return len(a.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws}))
		},
		"shelve": func(a *App) int {
			return len(a.handleShelveWorkspace(messages.ShelveWorkspace{Project: project, Workspace: ws}))
		},
		"restore": func(a *App) int {
			return len(a.handleRestoreWorkspace(messages.RestoreWorkspace{Project: project, Workspace: ws}))
		},
	}
	for name, dispatch := range dispatches {
		t.Run(name, func(t *testing.T) {
			app := lifecycleTestApp(t)
			if dispatch(app) == 0 {
				t.Fatal("first dispatch should emit commands")
			}
			if !app.isWorkspaceMutationInFlight(string(ws.ID())) {
				t.Fatal("dispatch must mark the workspace mutation-in-flight")
			}
			if got := dispatch(app); got != 0 {
				t.Fatalf("second dispatch while in-flight must be rejected, got %d cmds", got)
			}
		})
	}
}

// TestLifecycleResultClearsInFlight runs delete/shelve/restore through their
// success and failure handlers and asserts the mark is released so a retry
// dispatch is accepted again.
func TestLifecycleResultClearsInFlight(t *testing.T) {
	ws, project := lifecycleTestWorkspace()

	t.Run("delete success", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})
		app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceDeleted must clear the in-flight mark")
		}
		if len(app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})) == 0 {
			t.Fatal("dispatch after success must be accepted")
		}
	})
	t.Run("delete failure", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})
		app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{Workspace: ws, Err: errors.New("boom")})
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceDeleteFailed must clear the in-flight mark")
		}
	})
	t.Run("shelve success", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleShelveWorkspace(messages.ShelveWorkspace{Project: project, Workspace: ws})
		app.handleWorkspaceShelved(messages.WorkspaceShelved{Workspace: ws})
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceShelved must clear the in-flight mark")
		}
	})
	t.Run("shelve failure", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleShelveWorkspace(messages.ShelveWorkspace{Project: project, Workspace: ws})
		if cmd := app.handleWorkspaceShelveFailed(messages.WorkspaceShelveFailed{Workspace: ws, Err: errors.New("boom")}); cmd == nil {
			t.Fatal("shelve failure must surface an error command")
		}
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceShelveFailed must clear the in-flight mark")
		}
	})
	t.Run("restore success", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleRestoreWorkspace(messages.RestoreWorkspace{Project: project, Workspace: ws})
		app.handleWorkspaceRestored(messages.WorkspaceRestored{Workspace: ws})
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceRestored must clear the in-flight mark")
		}
	})
	t.Run("restore failure", func(t *testing.T) {
		app := lifecycleTestApp(t)
		app.handleRestoreWorkspace(messages.RestoreWorkspace{Project: project, Workspace: ws})
		if cmd := app.handleWorkspaceRestoreFailed(messages.WorkspaceRestoreFailed{Workspace: ws, Err: errors.New("boom")}); cmd == nil {
			t.Fatal("restore failure must surface an error command")
		}
		if app.isWorkspaceMutationInFlight(string(ws.ID())) {
			t.Fatal("WorkspaceRestoreFailed must clear the in-flight mark")
		}
	})
}

// TestLifecycleDispatchCrossOpGuard proves the phases are mutually exclusive:
// a delete in flight blocks a shelve or restore dispatch for the same
// workspace (and vice versa) — they share the markMutating phase.
func TestLifecycleDispatchCrossOpGuard(t *testing.T) {
	ws, project := lifecycleTestWorkspace()
	app := lifecycleTestApp(t)

	app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: ws})
	if got := len(app.handleShelveWorkspace(messages.ShelveWorkspace{Project: project, Workspace: ws})); got != 0 {
		t.Fatalf("shelve during delete must be rejected, got %d cmds", got)
	}
	if got := len(app.handleRestoreWorkspace(messages.RestoreWorkspace{Project: project, Workspace: ws})); got != 0 {
		t.Fatalf("restore during delete must be rejected, got %d cmds", got)
	}
}

// TestHandleWorkspaceDeleted_TombstoneAndCleanup asserts the confirmed-delete
// bookkeeping: tombstone under every stamped identity, dirty/active entries
// dropped, projects reload emitted.
func TestHandleWorkspaceDeleted_TombstoneAndCleanup(t *testing.T) {
	ws, _ := lifecycleTestWorkspace()
	wsID := string(ws.ID())
	app := lifecycleTestApp(t)
	app.lifecycle.dirty[wsID] = true
	app.tmuxActivity.activeWorkspaceIDs[wsID] = true
	app.lifecycle.projectsLoadToken = 3

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws, WorkspaceIDs: []string{wsID}})

	if !app.lifecycle.shouldFilterDeletedWorkspace(wsID, ws.Root, 3) {
		t.Fatal("deleted workspace must be tombstoned until the post-delete load")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("dirty marker must be cleared on confirmed delete")
	}
	if app.tmuxActivity.activeWorkspaceIDs[wsID] {
		t.Fatal("activeWorkspaceIDs entry must be cleared on confirmed delete")
	}
	if app.lifecycle.projectsLoadToken <= 3 {
		t.Fatal("confirmed delete must emit a projects reload")
	}
}

// TestHandleWorkspaceShelved_TearsDownModels pins the UI forwarding:
// a confirmed shelve must reach center and sidebarTerminal so tabs keyed to
// the killed tmux sessions don't resurface on restore.
func TestHandleWorkspaceShelved_TearsDownModels(t *testing.T) {
	ws, project := lifecycleTestWorkspace()
	wsID := string(ws.ID())
	app := lifecycleTestApp(t)

	app.center.SetWorkspace(ws)
	app.center.AddTab(&center.Tab{Name: "agent", Assistant: "claude", Workspace: ws})
	app.sidebarTerminal.AddTerminalForHarness(ws)
	if !strings.Contains(app.sidebarTerminal.TabBarView(), "Terminal 1") {
		t.Fatal("expected the harness terminal tab to render before shelve")
	}
	project.Workspaces = []data.Workspace{*ws}
	app.projects = []data.Project{*project}
	app.lifecycle.projectsLoadToken = 2

	app.handleWorkspaceShelved(messages.WorkspaceShelved{Workspace: ws, WorkspaceIDs: []string{wsID}})

	if tabs, _ := app.center.GetTabsInfoForWorkspace(wsID); len(tabs) != 0 {
		t.Fatalf("center must drop the shelved workspace's tabs, got %v", tabs)
	}
	if app.center.HasWorkspaceState(wsID) {
		t.Fatal("center must drop the shelved workspace's saved state")
	}
	if strings.Contains(app.sidebarTerminal.TabBarView(), "Terminal 1") {
		t.Fatal("sidebar terminal tab must be torn down on shelve")
	}
	if app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("in-flight mark must clear on confirmed shelve")
	}
	if !app.lifecycle.shouldFilterDeletedWorkspace(wsID, ws.Root, 2) {
		t.Fatal("shelved workspace must be tombstoned until reload")
	}
	if app.lifecycle.projectsLoadToken <= 2 {
		t.Fatal("confirmed shelve must emit a projects reload")
	}
}

// TestHandleWorkspaceRestored_Reloads asserts restore completion re-runs
// setup and reloads projects (cmds are the observable service calls).
func TestHandleWorkspaceRestored_Reloads(t *testing.T) {
	ws, _ := lifecycleTestWorkspace()
	app := lifecycleTestApp(t)
	app.lifecycle.markMutatingWorkspace(string(ws.ID()), ws.Root, true)

	cmds := app.handleWorkspaceRestored(messages.WorkspaceRestored{Workspace: ws})
	if app.isWorkspaceMutationInFlight(string(ws.ID())) {
		t.Fatal("restore completion must clear the in-flight mark")
	}
	var nonNil int
	for _, cmd := range cmds {
		if cmd != nil {
			nonNil++
		}
	}
	if nonNil < 2 {
		t.Fatalf("expected setup + reload commands from restore completion, got %d non-nil of %d", nonNil, len(cmds))
	}
	if app.lifecycle.projectsLoadToken == 0 {
		t.Fatal("restore completion must emit a projects reload")
	}
}

// --- Discovery results must not land mid-lifecycle -------------------

// TestDiscoveryResultDroppedWhileInFlight: a tmux discovery result arriving
// between dispatch and teardown must be dropped — attaching or filing tabs
// would race the kill pass and resurrect state under a tombstoned key.
func TestDiscoveryResultDroppedWhileInFlight(t *testing.T) {
	ws, project := lifecycleTestWorkspace()
	wsID := string(ws.ID())

	t.Run("sidebar attach dropped while deleting", func(t *testing.T) {
		app := lifecycleTestApp(t)
		project.Workspaces = []data.Workspace{*ws}
		app.projects = []data.Project{*project}
		app.activeWorkspace = ws
		app.markWorkspaceMutationInFlight(ws, true)

		cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
			WorkspaceID: wsID,
			Sessions:    []sidebar.SessionAttachInfo{{Name: "sess-1", Attach: true}},
		})
		if len(cmds) != 0 {
			t.Fatalf("discovery result for in-flight workspace must be dropped, got %d cmds", len(cmds))
		}
	})

	t.Run("sidebar attach dropped while tombstoned", func(t *testing.T) {
		app := lifecycleTestApp(t)
		project.Workspaces = []data.Workspace{*ws}
		app.projects = []data.Project{*project}
		app.activeWorkspace = ws
		app.lifecycle.projectsLoadToken = 5
		app.lifecycle.markDeletedUntilProjectsLoad(wsID, ws.Root, 5)

		cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
			WorkspaceID: wsID,
			Sessions:    []sidebar.SessionAttachInfo{{Name: "sess-1", Attach: true}},
		})
		if len(cmds) != 0 {
			t.Fatalf("discovery result for tombstoned workspace must be dropped, got %d cmds", len(cmds))
		}
	})

	t.Run("tabs discovery dropped while deleting", func(t *testing.T) {
		app := lifecycleTestApp(t)
		live := *ws
		project.Workspaces = []data.Workspace{live}
		app.projects = []data.Project{*project}
		app.markWorkspaceMutationInFlight(ws, true)

		cmds := app.handleTmuxTabsDiscoverResult(tmuxTabsDiscoverResult{
			WorkspaceID: wsID,
			Tabs:        []data.TabInfo{{Name: "agent", SessionName: "sess-1"}},
		})
		if len(cmds) != 0 {
			t.Fatalf("tabs discovery for in-flight workspace must be dropped, got %d cmds", len(cmds))
		}
		if len(app.projects[0].Workspaces[0].OpenTabs) != 0 {
			t.Fatal("dropped tabs discovery must not mutate OpenTabs")
		}
	})

	t.Run("normal workspace still attaches", func(t *testing.T) {
		app := lifecycleTestApp(t)
		project.Workspaces = []data.Workspace{*ws}
		app.projects = []data.Project{*project}
		app.activeWorkspace = ws

		cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
			WorkspaceID: wsID,
			Sessions:    []sidebar.SessionAttachInfo{{Name: "sess-1", Attach: true}},
		})
		if len(cmds) == 0 {
			t.Fatal("discovery result for a live workspace must still attach")
		}
	})
}
