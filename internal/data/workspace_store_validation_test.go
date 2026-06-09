package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStore_SaveRejectsNilWorkspace(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	if err := store.Save(nil); err == nil {
		t.Fatalf("expected Save to reject nil workspace")
	}
}

func TestWorkspaceStore_SaveRejectsWorkspaceWithMissingIdentity(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	if err := store.Save(&Workspace{Name: "missing-repo", Root: "/root"}); err == nil {
		t.Fatalf("expected Save to reject workspace with empty repo")
	}
	if err := store.Save(&Workspace{Name: "missing-root", Repo: "/repo"}); err == nil {
		t.Fatalf("expected Save to reject workspace with empty root")
	}
}

func TestWorkspaceStore_LoadMetadataForRejectsNilWorkspace(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	found, err := store.LoadMetadataFor(nil)
	if err == nil {
		t.Fatalf("expected LoadMetadataFor to reject nil workspace")
	}
	if found {
		t.Fatalf("expected found=false when workspace is nil")
	}
}

func TestWorkspaceStore_LockFileIsOutsideWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	id := WorkspaceID("abc123")

	lockPath := filepath.Clean(store.workspaceLockPath(id))
	workspaceDir := filepath.Clean(filepath.Join(root, string(id)))
	legacyLockPath := filepath.Clean(filepath.Join(workspaceDir, ".lock"))

	if filepath.Clean(filepath.Dir(lockPath)) != filepath.Clean(root) {
		t.Fatalf("expected lock file parent to be metadata root, got %s", filepath.Dir(lockPath))
	}
	if lockPath == legacyLockPath {
		t.Fatalf("lock path should not be inside workspace directory: %s", lockPath)
	}
}

func TestWorkspaceStore_LockWorkspaceIDsUsesDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	lowID := WorkspaceID("a")
	highID := WorkspaceID("b")

	heldHigh, err := lockRegistryFile(store.workspaceLockPath(highID), false)
	if err != nil {
		t.Fatalf("lockRegistryFile(highID) error = %v", err)
	}
	defer func() {
		unlockRegistryFile(heldHigh)
	}()

	done := make(chan error, 1)
	go func() {
		locks, lockErr := store.lockWorkspaceIDs(highID, lowID)
		if lockErr != nil {
			done <- lockErr
			return
		}
		unlockRegistryFiles(locks)
		done <- nil
	}()

	time.Sleep(100 * time.Millisecond)

	type lockResult struct {
		file *os.File
		err  error
	}
	lowAcquired := make(chan lockResult, 1)
	go func() {
		lowFile, lockErr := lockRegistryFile(store.workspaceLockPath(lowID), false)
		if lockErr != nil {
			lowAcquired <- lockResult{err: lockErr}
			return
		}
		lowAcquired <- lockResult{file: lowFile}
	}()

	select {
	case result := <-lowAcquired:
		if result.err != nil {
			t.Fatalf("lockRegistryFile(lowID) error = %v", result.err)
		}
		if result.file != nil {
			unlockRegistryFile(result.file)
		}
		t.Fatalf("expected lowID lock to be held while waiting on highID")
	case <-time.After(100 * time.Millisecond):
		// Expected: lowID lock is already held by lockWorkspaceIDs.
	}

	unlockRegistryFile(heldHigh)
	heldHigh = nil

	select {
	case lockErr := <-done:
		if lockErr != nil {
			t.Fatalf("lockWorkspaceIDs() error = %v", lockErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("lockWorkspaceIDs() did not complete after releasing highID lock")
	}

	select {
	case result := <-lowAcquired:
		if result.err != nil {
			t.Fatalf("lockRegistryFile(lowID) error = %v", result.err)
		}
		if result.file != nil {
			unlockRegistryFile(result.file)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("lockRegistryFile(lowID) did not complete after releasing ordered locks")
	}
}
