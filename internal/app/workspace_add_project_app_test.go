package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// These tests exercise the app's addProject path (the service called through
// the App wrapper) — they moved with the service tests but construct App, so
// they live here.

func TestAddProjectRejectsFakeGitDirectory(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	fakeRepo := filepath.Join(root, "fake-repo")
	if err := os.MkdirAll(filepath.Join(fakeRepo, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	registry := data.NewRegistry(filepath.Join(root, "projects.json"))
	service := workspacesvc.New(registry, nil, nil, "")
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

func TestAddProjectRejectsInvalidPath(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := workspacesvc.New(registry, nil, nil, "")
	app := &App{workspaceService: service}

	filePath := filepath.Join(tmp, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msg := app.addProject(filePath)()
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

func TestAddProjectRegistersGitRepo(t *testing.T) {
	skipIfNoGit(t)
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := workspacesvc.New(registry, nil, nil, "")
	app := &App{workspaceService: service}

	msg := app.addProject(repo)()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard, got %T", msg)
	}
	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected one registered project, got %d", len(paths))
	}
	if normalizePath(paths[0]) != normalizePath(repo) {
		t.Fatalf("registered path = %s, want %s", paths[0], repo)
	}
}

func TestAddProjectExpandsTildePath(t *testing.T) {
	skipIfNoGit(t)
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo): %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	t.Setenv("HOME", home)

	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := workspacesvc.New(registry, nil, nil, "")
	app := &App{workspaceService: service}

	msg := app.addProject("~/repo")()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard, got %T", msg)
	}
	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected one registered project, got %d", len(paths))
	}
	if normalizePath(paths[0]) != normalizePath(repo) {
		t.Fatalf("registered path = %s, want %s", paths[0], repo)
	}
}
