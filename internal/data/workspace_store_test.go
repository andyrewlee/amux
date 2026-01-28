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

func TestWorkspaceStore_ListByRepo_SkipsEmptyRoot(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	repo := "/home/user/repo"

	// Create a normal workspace with Root
	ws1 := &Workspace{Name: "ws1", Repo: repo, Root: "/path/to/ws1"}
	if err := store.Save(ws1); err != nil {
		t.Fatalf("Save(ws1) error = %v", err)
	}

	// Simulate a legacy workspace file with empty Root (manually write)
	// Legacy files only had metadata, no Root/Repo
	legacyID := "legacy123abc"
	dir := filepath.Join(root, legacyID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	legacyJSON := `{
		"repo": "/home/user/repo",
		"assistant": "claude"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// ListByRepo should only return the workspace with non-empty Root
	workspaces, err := store.ListByRepo(repo)
	if err != nil {
		t.Fatalf("ListByRepo() error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace (legacy with empty Root should be skipped), got %d", len(workspaces))
	}
	if workspaces[0].Name != "ws1" {
		t.Errorf("expected ws1, got %s", workspaces[0].Name)
	}
}

func TestWorkspaceStore_NormalizesRuntime(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Create workspace with empty runtime
	ws := &Workspace{
		Name:    "test",
		Repo:    "/repo",
		Root:    "/root",
		Runtime: "",
	}

	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Runtime should be normalized to local-worktree
	if loaded.Runtime != RuntimeLocalWorktree {
		t.Errorf("Runtime = %v, want %v", loaded.Runtime, RuntimeLocalWorktree)
	}
}

func TestWorkspaceStore_InitializesNilEnv(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name: "test",
		Repo: "/repo",
		Root: "/root",
		Env:  nil, // explicitly nil
	}

	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Env should not be nil after loading
	if loaded.Env == nil {
		t.Error("Env should not be nil after loading")
	}
}

func TestWorkspaceStore_LoadLegacyCreatedFormat(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Create a workspace with old format (Created as RFC3339 string)
	ws := &Workspace{
		Name: "legacy-test",
		Repo: "/repo",
		Root: "/root",
	}
	id := ws.ID()

	// Manually write old-format JSON with Created as string
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldFormat := `{
		"name": "legacy-test",
		"repo": "/repo",
		"root": "/root",
		"created": "2024-01-15T10:30:00Z"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(oldFormat), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load should work with old format
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify Created was parsed correctly
	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !loaded.Created.Equal(expectedTime) {
		t.Errorf("Created = %v, want %v", loaded.Created, expectedTime)
	}
}

func TestWorkspaceStore_LoadAppliesDefaults(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Create a minimal workspace JSON (missing Assistant, ScriptMode, Env)
	ws := &Workspace{
		Name: "minimal-test",
		Repo: "/repo",
		Root: "/root",
	}
	id := ws.ID()

	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	minimalJSON := `{
		"name": "minimal-test",
		"repo": "/repo",
		"root": "/root",
		"branch": "main"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(minimalJSON), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load should apply defaults
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify defaults were applied
	if loaded.Assistant != "claude" {
		t.Errorf("Assistant = %v, want 'claude'", loaded.Assistant)
	}
	if loaded.ScriptMode != "nonconcurrent" {
		t.Errorf("ScriptMode = %v, want 'nonconcurrent'", loaded.ScriptMode)
	}
	if loaded.Env == nil {
		t.Error("Env should not be nil")
	}
	if loaded.Runtime != RuntimeLocalWorktree {
		t.Errorf("Runtime = %v, want %v", loaded.Runtime, RuntimeLocalWorktree)
	}
}
