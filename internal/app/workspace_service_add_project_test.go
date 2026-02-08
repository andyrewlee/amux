package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestAddProjectRejectsFakeGitDirectory(t *testing.T) {
	skipIfNoGit(t)

	root := t.TempDir()
	fakeRepo := filepath.Join(root, "fake-repo")
	if err := os.MkdirAll(filepath.Join(fakeRepo, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	registry := data.NewRegistry(filepath.Join(root, "projects.json"))
	service := newWorkspaceService(registry, nil, nil, "")
	app := &App{workspaceService: service}

	msg := app.addProject(fakeRepo)()
	if _, ok := msg.(messages.Error); !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}

	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected no registered projects, got %d", len(paths))
	}
}
