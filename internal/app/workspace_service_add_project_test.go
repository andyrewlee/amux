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

func TestAddProjectRediscoversManagedWorktreesAfterRemoval(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	workspacesRoot := filepath.Join(root, "workspaces")
	managedRoot := filepath.Join(workspacesRoot, filepath.Base(repo), "feature")
	if err := os.MkdirAll(filepath.Dir(managedRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "worktree", "add", "-b", "feature", managedRoot, "main")

	registry := data.NewRegistry(filepath.Join(root, "projects.json"))
	store := data.NewWorkspaceStore(filepath.Join(root, "metadata"))
	service := newWorkspaceService(registry, store, nil, workspacesRoot)
	msg := service.AddProject(repo)()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("first AddProject returned %T, want RefreshDashboard", msg)
	}
	msg = service.RemoveProject(data.NewProject(repo))()
	if _, ok := msg.(messages.ProjectRemoved); !ok {
		t.Fatalf("RemoveProject returned %T, want ProjectRemoved", msg)
	}
	if workspaces, err := store.ListByRepo(repo); err != nil || len(workspaces) != 0 {
		t.Fatalf("metadata after removal = %v, %v; want none", workspaces, err)
	}
	if _, err := os.Stat(managedRoot); err != nil {
		t.Fatalf("managed worktree was removed from disk: %v", err)
	}

	msg = service.AddProject(repo)()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("second AddProject returned %T, want RefreshDashboard", msg)
	}
	workspaces, err := store.ListByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	foundManaged := false
	for _, ws := range workspaces {
		if normalizePath(ws.Root) == normalizePath(managedRoot) {
			foundManaged = true
			break
		}
	}
	if !foundManaged {
		t.Fatalf("re-added project did not rediscover managed worktree %s: %+v", managedRoot, workspaces)
	}

	// Simulate a delayed cleanup from another amux process that began before the
	// re-add. Because the registry now contains the project, cleanup must restore
	// the metadata it just removed instead of clobbering the later re-add.
	service.removeProjectMetadata(repo)
	workspaces, err = store.ListByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	foundManaged = false
	for _, ws := range workspaces {
		if normalizePath(ws.Root) == normalizePath(managedRoot) {
			foundManaged = true
			break
		}
	}
	if !foundManaged {
		t.Fatalf("late metadata cleanup clobbered re-added worktree %s: %+v", managedRoot, workspaces)
	}
}
