package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStore_SaveLoadDelete(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name:       "test-workspace",
		Branch:     "test-branch",
		Base:       "origin/main",
		Repo:       "/home/user/repo",
		Root:       "/home/user/.amux/workspaces/test-workspace",
		Created:    time.Now(),
		Runtime:    RuntimeLocalWorktree,
		Assistant:  "claude",
		ScriptMode: "nonconcurrent",
		Env:        map[string]string{"FOO": "bar"},
	}

	// Save
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	id := ws.ID()
	path := filepath.Join(root, string(id), "workspace.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected workspace file to exist: %v", err)
	}

	// Load
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Name != ws.Name {
		t.Errorf("Name = %v, want %v", loaded.Name, ws.Name)
	}
	if loaded.Branch != ws.Branch {
		t.Errorf("Branch = %v, want %v", loaded.Branch, ws.Branch)
	}
	if loaded.Runtime != ws.Runtime {
		t.Errorf("Runtime = %v, want %v", loaded.Runtime, ws.Runtime)
	}
	if loaded.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %v, want bar", loaded.Env["FOO"])
	}

	// Delete
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected workspace file to be deleted, err=%v", err)
	}
}

func TestWorkspaceStore_List(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Empty list
	ids, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 workspaces, got %d", len(ids))
	}

	// Create two workspaces
	ws1 := &Workspace{
		Name: "ws1",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/ws1",
	}
	ws2 := &Workspace{
		Name: "ws2",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/ws2",
	}

	if err := store.Save(ws1); err != nil {
		t.Fatalf("Save(ws1) error = %v", err)
	}
	if err := store.Save(ws2); err != nil {
		t.Fatalf("Save(ws2) error = %v", err)
	}

	ids, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(ids))
	}
}

func TestWorkspaceStore_ListByRepo(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	repo1 := "/home/user/repo1"
	repo2 := "/home/user/repo2"

	// Create workspaces in different repos
	ws1 := &Workspace{Name: "ws1", Repo: repo1, Root: "/path/to/ws1"}
	ws2 := &Workspace{Name: "ws2", Repo: repo1, Root: "/path/to/ws2"}
	ws3 := &Workspace{Name: "ws3", Repo: repo2, Root: "/path/to/ws3"}

	for _, ws := range []*Workspace{ws1, ws2, ws3} {
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%s) error = %v", ws.Name, err)
		}
	}

	// List by repo1
	workspaces, err := store.ListByRepo(repo1)
	if err != nil {
		t.Fatalf("ListByRepo() error = %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces for repo1, got %d", len(workspaces))
	}

	// List by repo2
	workspaces, err = store.ListByRepo(repo2)
	if err != nil {
		t.Fatalf("ListByRepo() error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace for repo2, got %d", len(workspaces))
	}
}

func TestWorkspaceStore_LoadNotFound(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	_, err := store.Load("nonexistent")
	if !os.IsNotExist(err) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestWorkspaceStore_ListByRepo_SkipsArchived(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	repo := "/home/user/repo"

	active := &Workspace{Name: "active", Repo: repo, Root: "/path/to/active"}
	archived := &Workspace{Name: "old", Repo: repo, Root: "/path/to/old", Archived: true}

	if err := store.Save(active); err != nil {
		t.Fatalf("Save(active) error = %v", err)
	}
	if err := store.Save(archived); err != nil {
		t.Fatalf("Save(archived) error = %v", err)
	}

	workspaces, err := store.ListByRepo(repo)
	if err != nil {
		t.Fatalf("ListByRepo() error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace after skipping archived, got %d", len(workspaces))
	}
	if workspaces[0].Name != "active" {
		t.Errorf("expected active workspace, got %s", workspaces[0].Name)
	}
}
