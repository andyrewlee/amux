package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestHandleProjectsLoaded_DropsStaleLoadToken proves an out-of-order reload is
// dropped: under rapid deletes an older LoadProjects goroutine (started before a
// later delete completed) could deliver last and resurrect the deleted workspace.
func TestHandleProjectsLoaded_DropsStaleLoadToken(t *testing.T) {
	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
	newer := []data.Project{{Name: "kept", Path: "/kept"}}
	older := []data.Project{{Name: "kept", Path: "/kept"}, {Name: "deleted", Path: "/deleted"}}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: newer, LoadToken: 5})
	if len(app.projects) != 1 {
		t.Fatalf("expected newer (higher-token) set applied, got %d projects", len(app.projects))
	}

	// A later-arriving but OLDER load (lower token) must be dropped.
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: older, LoadToken: 3})
	if len(app.projects) != 1 {
		t.Fatalf("stale lower-token load must be dropped, got %d projects", len(app.projects))
	}

	// A zero-token message still applies (back-compat with existing callers/tests).
	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: older, LoadToken: 0})
	if len(app.projects) != 2 {
		t.Fatalf("zero-token load should apply, got %d projects", len(app.projects))
	}
}

func TestHandleProjectsLoaded_FiltersDeletingWorkspace(t *testing.T) {
	repo := "/repo"
	deleted := data.NewWorkspace("deleted", "deleted", "main", repo, "/repo/deleted")
	kept := data.NewWorkspace("kept", "kept", "main", repo, "/repo/kept")
	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*deleted, *kept}

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		lifecycle:       newWorkspaceLifecycleState(),
	}
	app.markWorkspaceDeleteInFlight(deleted, true)

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}, LoadToken: 1})

	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected one surviving workspace, got %+v", app.projects)
	}
	if got := app.projects[0].Workspaces[0].Root; got != kept.Root {
		t.Fatalf("expected surviving workspace %q, got %q", kept.Root, got)
	}
}

func TestHandleProjectsLoaded_PreservesActiveDeletingWorkspace(t *testing.T) {
	repo := "/repo"
	deleting := data.NewWorkspace("deleting", "deleting", "main", repo, "/repo/deleting")
	kept := data.NewWorkspace("kept", "kept", "main", repo, "/repo/kept")
	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*deleting, *kept}

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		lifecycle:       newWorkspaceLifecycleState(),
		activeProject:   project,
		activeWorkspace: deleting,
	}
	app.markWorkspaceDeleteInFlight(deleting, true)

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}, LoadToken: 1})

	if app.activeWorkspace == nil || app.activeWorkspace.Root != deleting.Root {
		t.Fatalf("expected active deleting workspace to stay put, got %+v", app.activeWorkspace)
	}
	if app.activeProject == nil || app.activeProject.Path != project.Path {
		t.Fatalf("expected active project to stay put, got %+v", app.activeProject)
	}
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 || app.projects[0].Workspaces[0].Root != kept.Root {
		t.Fatalf("expected deleting workspace hidden from project list, got %+v", app.projects)
	}
}

func TestHandleProjectsLoaded_FiltersDeletedWorkspaceUntilPostDeleteReload(t *testing.T) {
	repo := "/repo"
	deleted := data.NewWorkspace("deleted", "deleted", "main", repo, "/repo/deleted")
	kept := data.NewWorkspace("kept", "kept", "main", repo, "/repo/kept")
	projectWithDeleted := data.NewProject(repo)
	projectWithDeleted.Workspaces = []data.Workspace{*deleted, *kept}
	projectWithDeletedAgain := data.NewProject(repo)
	projectWithDeletedAgain.Workspaces = []data.Workspace{*deleted, *kept}
	projectWithoutDeleted := data.NewProject(repo)
	projectWithoutDeleted.Workspaces = []data.Workspace{*kept}

	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		lifecycle:       newWorkspaceLifecycleState(),
	}
	app.lifecycle.markDeletedUntilProjectsLoad("pre-delete-resolved-id", deleted.Root, 5)

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*projectWithDeleted}, LoadToken: 4})
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected stale pre-barrier load to hide deleted workspace, got %+v", app.projects)
	}
	if got := app.projects[0].Workspaces[0].Root; got != kept.Root {
		t.Fatalf("expected surviving workspace %q, got %q", kept.Root, got)
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*projectWithDeletedAgain}, LoadToken: 5})
	if len(app.lifecycle.deletedUntilProjectsLoadToken) == 0 {
		t.Fatal("expected barrier to remain while post-delete load still contains deleted workspace")
	}
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 || app.projects[0].Workspaces[0].Root != kept.Root {
		t.Fatalf("expected stale post-delete load to keep hiding deleted workspace, got %+v", app.projects)
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*projectWithoutDeleted}, LoadToken: 6})
	if len(app.lifecycle.deletedUntilProjectsLoadToken) != 0 {
		t.Fatalf("expected post-delete load to clear deleted workspace barrier, got %+v", app.lifecycle.deletedUntilProjectsLoadToken)
	}
}
