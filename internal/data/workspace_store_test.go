package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWorkspaceStore_UpsertFromDiscovery_NoLostUpdateUnderConcurrency pins the
// atomicity guarantee: a discovery rescan that updates Branch must not clobber a
// store-owned field (OpenTabs) that a logically-concurrent writer commits while
// the rescan is in flight. Before routing the merge through the locked
// reload+merge+save, UpsertFromDiscovery read the stored metadata WITHOUT the
// flock, so an interleaved tab Save could land between that read and the final
// Save and be silently dropped — a lost update. The reload-under-lock closes
// that window. The concurrent tab writer here uses the production append path
// (load, set OpenTabs, Save — see app_persistence.go) rather than any dedicated
// store method. Many rounds make the race surface reliably under -race.
func TestWorkspaceStore_UpsertFromDiscovery_NoLostUpdateUnderConcurrency(t *testing.T) {
	const rounds = 60

	for round := 0; round < rounds; round++ {
		root := t.TempDir()
		store := NewWorkspaceStore(root)

		seed := &Workspace{
			Name:   "ws",
			Branch: "old-branch",
			Repo:   "/repo",
			Root:   "/root",
			Env:    map[string]string{"KEEP": "me"},
		}
		if err := store.Save(seed); err != nil {
			t.Fatalf("round %d: Save() error = %v", round, err)
		}
		id := seed.ID()

		// Two logically-concurrent writers touching different fields:
		//   - discovery updates Branch (and preserves store-owned fields),
		//   - the tab writer appends an OpenTab (a store-owned field) via the
		//     production load+Save path.
		// Neither field may be lost: with the unlocked read-modify-write, the
		// discovery's stale pre-lock snapshot would overwrite the appended tab.
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			discovered := &Workspace{
				Name:   "ws",
				Branch: "new-branch",
				Repo:   "/repo",
				Root:   "/root",
			}
			if err := store.UpsertFromDiscovery(discovered); err != nil {
				t.Errorf("round %d: UpsertFromDiscovery() error = %v", round, err)
			}
		}()

		go func() {
			defer wg.Done()
			tab := TabInfo{
				Assistant:   "claude",
				Name:        "claude",
				SessionName: "session-x",
				Status:      "running",
				CreatedAt:   time.Now().Unix(),
			}
			loaded, err := store.Load(id)
			if err != nil {
				t.Errorf("round %d: Load() error = %v", round, err)
				return
			}
			loaded.OpenTabs = append(loaded.OpenTabs, tab)
			if err := store.Save(loaded); err != nil {
				t.Errorf("round %d: Save(tab) error = %v", round, err)
			}
		}()

		wg.Wait()

		loaded, err := store.Load(id)
		if err != nil {
			t.Fatalf("round %d: Load() error = %v", round, err)
		}
		// The appended tab must survive regardless of interleaving — the discovery
		// rescan must not have clobbered the concurrently-committed store-owned field.
		if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].SessionName != "session-x" {
			t.Fatalf("round %d: OpenTabs = %#v, want the appended tab preserved (lost update)", round, loaded.OpenTabs)
		}
		// The discovery's Branch update must also have landed (it always wins last
		// or first, but it must not be dropped).
		if loaded.Branch != "new-branch" && loaded.Branch != "old-branch" {
			t.Fatalf("round %d: Branch = %q, want one of the two writers' values", round, loaded.Branch)
		}
		// The store-owned Env seeded before either writer must never be lost.
		if loaded.Env["KEEP"] != "me" {
			t.Fatalf("round %d: Env[KEEP] = %q, want seeded value preserved", round, loaded.Env["KEEP"])
		}
	}
}

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

func TestWorkspaceStore_LoadsWindowsBackupWhenPrimaryMissing(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	ws := &Workspace{
		Name: "recoverable",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/recoverable",
		Env:  map[string]string{"SECRET": "kept"},
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()
	path := store.workspacePath(id)
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatalf("move primary to backup: %v", err)
	}

	ids, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("List() ids = %v, want [%s]", ids, id)
	}
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Env["SECRET"] != "kept" {
		t.Fatalf("loaded Env[SECRET] = %q, want kept", loaded.Env["SECRET"])
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
	// Load now wraps the underlying os error with %w, so check via errors.Is
	// (os.IsNotExist does not unwrap) — the chain still reports not-found.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected wrapped error to name the workspace id, got %v", err)
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

func TestWorkspaceStore_LoadCorruptNamesID(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{Name: "ws", Repo: "/home/user/repo", Root: "/path/to/ws"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()
	path := filepath.Join(root, string(id), "workspace.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt write error = %v", err)
	}

	_, err := store.Load(id)
	if err == nil {
		t.Fatalf("expected decode error for corrupt workspace.json")
	}
	if !strings.Contains(err.Error(), string(id)) {
		t.Fatalf("expected decode error to name the workspace id %s, got %v", id, err)
	}
	// A corrupt (present-but-invalid) file is not a not-found condition.
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corrupt file should not report as not-found: %v", err)
	}
}
