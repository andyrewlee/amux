package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestAddProjectRejectsInvalidPath(t *testing.T) {
	tmp := t.TempDir()
	registry := data.NewRegistry(filepath.Join(tmp, "projects.json"))
	service := newWorkspaceService(registry, nil, nil, "")
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
	service := newWorkspaceService(registry, nil, nil, "")
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
	service := newWorkspaceService(registry, nil, nil, "")
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

func TestCreateWorkspaceRejectsInvalidName(t *testing.T) {
	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })

	var createCalled bool
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return nil
	}

	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.CreateWorkspace(project, "bad/name", "main")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected pending workspace in validation failure")
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if createCalled {
		t.Fatal("createWorkspaceFn should not have been called")
	}
}

func TestCreateWorkspaceRejectsInvalidBaseRef(t *testing.T) {
	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })

	var createCalled bool
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return nil
	}

	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.CreateWorkspace(project, "feature", "bad ref")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected pending workspace in validation failure")
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if createCalled {
		t.Fatal("createWorkspaceFn should not have been called")
	}
}

func TestCreateWorkspaceRejectsPathOutsideManagedRoot(t *testing.T) {
	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })

	var createCalled bool
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		createCalled = true
		return nil
	}

	// Use a project name with ".." to try to escape the managed root.
	// projectNameSegment rejects ".." in the name, so isManagedWorkspacePathForProject fails.
	project := &data.Project{Name: "../escape", Path: "/tmp/repo"}
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	msg := svc.CreateWorkspace(project, "feature", "main")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "outside managed project root") {
		t.Fatalf("expected 'outside managed project root' error, got: %v", failed.Err)
	}
	if createCalled {
		t.Fatal("createWorkspaceFn should not have been called")
	}
}
