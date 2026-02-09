package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleProjectsLoadedRebindsActiveWorkspace(t *testing.T) {
	repo := t.TempDir()
	oldWorkspace := data.NewWorkspace("feature", "feature", "HEAD", repo, t.TempDir())
	oldWorkspace.Assistant = "claude"
	oldProject := data.NewProject(repo)
	oldProject.Workspaces = []data.Workspace{*oldWorkspace}

	app := &App{
		dashboard:       dashboard.New(),
		activeProject:   oldProject,
		activeWorkspace: oldWorkspace,
		showWelcome:     false,
	}

	updatedWorkspace := data.NewWorkspace("feature", "feature", "HEAD", repo, oldWorkspace.Root)
	updatedWorkspace.Assistant = "codex"
	updatedProject := data.NewProject(repo)
	updatedProject.Workspaces = []data.Workspace{*updatedWorkspace}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*updatedProject}})

	if app.activeWorkspace == nil {
		t.Fatalf("expected active workspace to remain selected")
	}
	if app.activeWorkspace == oldWorkspace {
		t.Fatalf("expected active workspace pointer to rebind to loaded project data")
	}
	if app.activeWorkspace.Assistant != "codex" {
		t.Fatalf("assistant = %q, want codex", app.activeWorkspace.Assistant)
	}
	if app.activeProject == nil || app.activeProject.Path != repo {
		t.Fatalf("expected active project to rebind to loaded project")
	}
	if app.showWelcome {
		t.Fatalf("expected app to remain in workspace view")
	}
}

func TestHandleProjectsLoadedClearsMissingActiveWorkspace(t *testing.T) {
	repo := t.TempDir()
	activeWorkspace := data.NewWorkspace("feature", "feature", "HEAD", repo, t.TempDir())
	activeProject := data.NewProject(repo)
	activeProject.Workspaces = []data.Workspace{*activeWorkspace}

	app := &App{
		dashboard:       dashboard.New(),
		activeProject:   activeProject,
		activeWorkspace: activeWorkspace,
		showWelcome:     false,
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{})

	if app.activeWorkspace != nil {
		t.Fatalf("expected active workspace to be cleared when not present in reload")
	}
	if app.activeProject != nil {
		t.Fatalf("expected active project to be cleared when active workspace disappears")
	}
	if !app.showWelcome {
		t.Fatalf("expected app to return to home view")
	}
}

func TestHandleProjectsLoadedRebindsActiveProjectByCanonicalPath(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(%s) error = %v", base, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	relativeProject := &data.Project{Name: "repo", Path: "./repo"}
	reloadedProject := data.NewProject(repo)

	app := &App{
		dashboard:     dashboard.New(),
		activeProject: relativeProject,
		showWelcome:   true,
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*reloadedProject}})

	if app.activeProject == nil {
		t.Fatalf("expected active project to remain selected")
	}
	if app.activeProject.Path != reloadedProject.Path {
		t.Fatalf("active project path = %q, want %q", app.activeProject.Path, reloadedProject.Path)
	}
}
