package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestFindWorkspaceByID_PrefersActiveWorkspace(t *testing.T) {
	repo := t.TempDir()
	active := data.NewWorkspace("feature", "feature", "main", repo, repo)
	projectCopy := *active

	app := &App{
		activeWorkspace: active,
		projects: []data.Project{
			{
				Path:       repo,
				Workspaces: []data.Workspace{projectCopy},
			},
		},
	}

	got := app.findWorkspaceByID(string(active.ID()))
	if got != active {
		t.Fatalf("expected active workspace pointer, got %p want %p", got, active)
	}
}

func TestFindWorkspaceByID_FallsBackToProjects(t *testing.T) {
	repo := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)

	app := &App{
		projects: []data.Project{
			{
				Path:       repo,
				Workspaces: []data.Workspace{*ws},
			},
		},
	}

	got := app.findWorkspaceByID(string(ws.ID()))
	if got == nil {
		t.Fatal("expected workspace lookup to return project workspace")
	}
	if string(got.ID()) != string(ws.ID()) {
		t.Fatalf("workspace id = %q, want %q", got.ID(), ws.ID())
	}
}
