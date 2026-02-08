package data

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestWorkspaceStore_LoadRejectsInvalidWorkspaceID(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	if _, err := store.Load(""); err == nil {
		t.Fatalf("expected Load to reject empty workspace id")
	}
	if _, err := store.Load(WorkspaceID("../escape")); err == nil {
		t.Fatalf("expected Load to reject traversal workspace id")
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldFormat := `{
		"name": "legacy-test",
		"repo": "/repo",
		"root": "/root",
		"created": "2024-01-15T10:30:00Z"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(oldFormat), 0o644); err != nil {
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	minimalJSON := `{
		"name": "minimal-test",
		"repo": "/repo",
		"root": "/root",
		"branch": "main"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(minimalJSON), 0o644); err != nil {
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

func TestWorkspaceStore_ListByRepo_NormalizesSymlinks(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	base := t.TempDir()
	repoReal := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	repoLink := filepath.Join(base, "repo-link")
	if err := os.Symlink(repoReal, repoLink); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	rootReal := filepath.Join(repoReal, ".amux", "workspaces", "feature")
	if err := os.MkdirAll(rootReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootReal) error = %v", err)
	}
	rootLink := filepath.Join(repoLink, ".amux", "workspaces", "feature")

	wsReal := &Workspace{Name: "feature", Repo: repoReal, Root: rootReal}
	wsLink := &Workspace{Name: "feature", Repo: repoLink, Root: rootLink}

	if err := store.Save(wsReal); err != nil {
		t.Fatalf("Save(wsReal) error = %v", err)
	}
	if err := store.Save(wsLink); err != nil {
		t.Fatalf("Save(wsLink) error = %v", err)
	}

	workspaces, err := store.ListByRepo(repoReal)
	if err != nil {
		t.Fatalf("ListByRepo() error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace after symlink normalization, got %d", len(workspaces))
	}
}

func TestWorkspaceStore_DeleteWaitsForWorkspaceLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows lock implementation is best-effort")
	}
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name: "locked-delete",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/locked-delete",
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()
	lockFile, err := lockRegistryFile(store.workspaceLockPath(id), false)
	if err != nil {
		t.Fatalf("lockRegistryFile() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.Delete(id)
	}()

	select {
	case err := <-done:
		t.Fatalf("Delete() should block on held lock, got %v", err)
	case <-time.After(100 * time.Millisecond):
		// Expected: delete blocks until lock is released.
	}

	unlockRegistryFile(lockFile)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Delete() did not complete after lock release")
	}

	if _, err := os.Stat(filepath.Join(root, string(id))); !os.IsNotExist(err) {
		t.Fatalf("expected workspace directory removed, stat err=%v", err)
	}
}

func TestWorkspaceStore_DeleteRejectsInvalidWorkspaceID(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	marker := filepath.Join(root, "marker.txt")
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := store.Delete(""); err == nil {
		t.Fatalf("expected Delete to reject empty workspace id")
	}
	if err := store.Delete(WorkspaceID("../escape")); err == nil {
		t.Fatalf("expected Delete to reject traversal workspace id")
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected metadata root to remain intact, stat err=%v", err)
	}
}

func TestWorkspaceStore_ListByRepo_IgnoresUnrelatedCorruptMetadata(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Write one corrupt metadata file for an unrelated workspace ID.
	corruptDir := filepath.Join(root, "deadbeefcafebabe")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corrupt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, workspaceFilename), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	repoWithWorkspace := filepath.Join(t.TempDir(), "repo-a")
	ws := &Workspace{Name: "ws-a", Repo: repoWithWorkspace, Root: filepath.Join(root, "workspaces", "ws-a")}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save(valid workspace) error = %v", err)
	}

	// Query a different repo with no matching workspaces. This should be an
	// empty result, not an error caused by unrelated corruption.
	repoWithoutWorkspace := filepath.Join(t.TempDir(), "repo-b")
	workspaces, err := store.ListByRepo(repoWithoutWorkspace)
	if err != nil {
		t.Fatalf("ListByRepo() should ignore unrelated corruption, got error: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("expected 0 workspaces for repo without metadata, got %d", len(workspaces))
	}
}
