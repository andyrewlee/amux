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

// TestWorkspaceStore_RemovesOldLockFileOnSaveRebind proves a rebind (ID change)
// removes the stale old lock file while keeping the live one.
func TestWorkspaceStore_RemovesOldLockFileOnSaveRebind(t *testing.T) {
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

	// Rebind: changing Root changes the computed ID while storeID still points at
	// the old metadata, triggering the oldID cleanup branch.
	ws.Root = "/home/user/.amux/workspaces/new-root"
	if err := store.Save(ws); err != nil {
		t.Fatalf("rebind Save() error = %v", err)
	}
	newID := ws.ID()
	if oldID == newID {
		t.Fatal("expected the workspace ID to change on rebind")
	}

	if _, err := os.Stat(store.workspaceLockPath(oldID)); !os.IsNotExist(err) {
		t.Fatalf("expected stale old lock file removed after rebind, stat err=%v", err)
	}
	if _, err := os.Stat(store.workspaceLockPath(newID)); err != nil {
		t.Fatalf("expected live new lock file retained, stat err=%v", err)
	}
}
