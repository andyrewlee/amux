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
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	oldWS.Assistant = "claude"
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
	}

	// Simulate reload with updated workspace (assistant changed)
	newWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	newWS.Assistant = "codex"
	newProject := data.NewProject(repo)
	newProject.AddWorkspace(*newWS)

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace == nil {
		t.Fatal("expected activeWorkspace to be rebound, got nil")
	}
	if app.activeWorkspace.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", app.activeWorkspace.Assistant)
	}
	if app.activeProject == nil {
		t.Fatal("expected activeProject to be rebound, got nil")
	}
	if app.showWelcome {
		t.Fatal("expected showWelcome to remain false after rebind")
	}
}

func TestHandleProjectsLoadedClearsMissingActiveWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.AddWorkspace(*oldWS)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*project},
		activeWorkspace: &project.Workspaces[0],
		activeProject:   project,
		showWelcome:     false,
	}

	// Reload with empty projects — workspace disappeared
	msg := messages.ProjectsLoaded{Projects: []data.Project{}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace != nil {
		t.Fatalf("expected activeWorkspace nil, got %+v", app.activeWorkspace)
	}
	if app.activeProject != nil {
		t.Fatalf("expected activeProject nil, got %+v", app.activeProject)
	}
	if !app.showWelcome {
		t.Fatal("expected showWelcome true after workspace disappeared")
	}
}

func TestHandleProjectsLoadedRebindsActiveProjectByCanonicalPath(t *testing.T) {
	// Use a relative path for the active project
	relPath := "./repo"
	absPath, err := filepath.Abs(relPath)
	if err != nil {
		t.Fatalf("Abs(%q): %v", relPath, err)
	}

	oldProject := &data.Project{Name: "repo", Path: relPath}

	app := &App{
		dashboard:     dashboard.New(),
		projects:      []data.Project{*oldProject},
		activeProject: oldProject,
		showWelcome:   true,
	}

	// Reload with absolute path version of the same project
	newProject := data.NewProject(absPath)
	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeProject == nil {
		t.Fatal("expected activeProject to be rebound via canonical path, got nil")
	}
	if app.activeProject.Path != absPath {
		t.Fatalf("expected activeProject.Path %q, got %q", absPath, app.activeProject.Path)
	}
}

func TestHandleProjectsLoadedRebindsActiveWorkspaceByCanonicalPathsOnIDMiss(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*oldProject},
		activeWorkspace: &oldProject.Workspaces[0],
		activeProject:   oldProject,
		showWelcome:     false,
	}

	// Simulate discovery rewriting relative workspace metadata to absolute paths.
	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newWS.Assistant = "codex"
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	msg := messages.ProjectsLoaded{Projects: []data.Project{*newProject}}
	app.handleProjectsLoaded(msg)

	if app.activeWorkspace == nil {
		t.Fatal("expected activeWorkspace to stay bound after ID miss fallback")
	}
	if app.activeWorkspace.Root != absRoot {
		t.Fatalf("expected activeWorkspace.Root %q, got %q", absRoot, app.activeWorkspace.Root)
	}
	if app.activeWorkspace.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", app.activeWorkspace.Assistant)
	}
	if app.activeProject == nil {
		t.Fatal("expected activeProject to stay bound after workspace rebind")
	}
	if app.activeProject.Path != absRepo {
		t.Fatalf("expected activeProject.Path %q, got %q", absRepo, app.activeProject.Path)
	}
	if app.showWelcome {
		t.Fatal("expected showWelcome to remain false after canonical path rebind")
	}
}
