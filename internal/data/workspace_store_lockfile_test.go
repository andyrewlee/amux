package data

import (
	"os"
	"testing"
)

// TestWorkspaceStore_RemovesLockFileOnDelete proves the sibling <id>.lock
// rendezvous file is cleaned up on Delete instead of leaking forever.
func TestWorkspaceStore_RemovesLockFileOnDelete(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name: "lockfile-delete",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/lockfile-delete",
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()
	lockPath := store.workspaceLockPath(id)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file created by Save, stat err=%v", err)
	}

	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file removed after Delete, stat err=%v", err)
	}
}

// TestWorkspaceStore_RootMutationKeepsPersistedKey proves that mutating Root
// after the first save does not rebind the record: the store
// key the identity, so the record — and its lock file — stay under the
// minted key while the moved path is saved into it.
func TestWorkspaceStore_RootMutationKeepsPersistedKey(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name: "lockfile-rebind",
		Repo: "/home/user/repo",
		Root: "/home/user/.amux/workspaces/old-root",
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	oldID := ws.ID()

	ws.Root = "/home/user/.amux/workspaces/new-root"
	if err := store.Save(ws); err != nil {
		t.Fatalf("rebind Save() error = %v", err)
	}
	if ws.ID() != oldID {
		t.Fatal("expected the persisted key to survive a Root mutation")
	}
	if ws.ComputedID() == oldID {
		t.Fatal("expected ComputedID to reflect the new root")
	}

	if _, err := os.Stat(store.workspaceLockPath(oldID)); err != nil {
		t.Fatalf("expected lock file retained under the persisted key, stat err=%v", err)
	}
	if _, err := os.Stat(store.workspaceLockPath(ws.ComputedID())); !os.IsNotExist(err) {
		t.Fatalf("expected no record under the computed key, stat err=%v", err)
	}
	loaded, err := store.Load(oldID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Root != ws.Root {
		t.Fatalf("loaded Root = %q, want %q", loaded.Root, ws.Root)
	}
}
