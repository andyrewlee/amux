package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestFindWorkspaceByID_PrefersActiveWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	ws := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.AddWorkspace(*ws)

	// activeWorkspace is a distinct pointer with the same identity
	active := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	active.Assistant = "codex"

	app := &App{
		projects:        []data.Project{*project},
		activeWorkspace: active,
	}

	found := app.findWorkspaceByID(string(ws.ID()))
	if found == nil {
		t.Fatal("expected workspace to be found")
	}
	if found != active {
		t.Fatal("expected findWorkspaceByID to prefer activeWorkspace pointer")
	}
	if found.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", found.Assistant)
	}
}

func TestFindWorkspaceByID_FallsBackToProjects(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	ws := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.AddWorkspace(*ws)

	app := &App{
		projects:        []data.Project{*project},
		activeWorkspace: nil, // no active workspace
	}

	found := app.findWorkspaceByID(string(ws.ID()))
	if found == nil {
		t.Fatal("expected workspace to be found via project scan")
	}
	if found.Name != "feature" {
		t.Fatalf("expected name %q, got %q", "feature", found.Name)
	}
}
