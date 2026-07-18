package data

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStorePruneStaleReconcilesOwnedState(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	managedRoot := filepath.Join(root, "workspaces")
	registeredRepo := filepath.Join(root, "registered")
	unregisteredRepo := filepath.Join(root, "removed")
	for _, path := range []string{managedRoot, registeredRepo, unregisteredRepo} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	keepRoot := filepath.Join(managedRoot, "repo", "keep")
	orphanRoot := filepath.Join(managedRoot, "repo", "orphan-files-stay")
	archivedRoot := filepath.Join(managedRoot, "repo", "archived")
	for _, path := range []string{keepRoot, orphanRoot, archivedRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	keep := NewWorkspace("keep", "keep", "main", registeredRepo, keepRoot)
	orphanOld := NewWorkspace("orphan-old", "old", "main", unregisteredRepo, orphanRoot)
	orphanFresh := NewWorkspace("orphan-fresh", "fresh", "main", unregisteredRepo, filepath.Join(orphanRoot, "fresh"))
	missing := NewWorkspace("missing", "missing", "main", registeredRepo, filepath.Join(managedRoot, "repo", "missing"))
	archived := NewWorkspace("archived", "archived", "main", registeredRepo, archivedRoot)
	archived.Archived = true
	archived.ArchivedAt = now.Add(-8 * 24 * time.Hour)

	for _, ws := range []*Workspace{keep, orphanOld, orphanFresh, missing, archived} {
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%s): %v", ws.Name, err)
		}
		mtime := now.Add(-2 * time.Hour)
		if ws == orphanFresh {
			mtime = now.Add(-30 * time.Minute)
		}
		if err := os.Chtimes(store.workspacePath(ws.ID()), mtime, mtime); err != nil {
			t.Fatalf("Chtimes(%s): %v", ws.Name, err)
		}
	}

	orphanLockID := WorkspaceID("orphan-lock")
	locks, err := store.lockWorkspaceIDs(orphanLockID)
	if err != nil {
		t.Fatalf("lockWorkspaceIDs: %v", err)
	}
	unlockRegistryFiles(locks)

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos:   []string{registeredRepo},
		ManagedRoot:       managedRoot,
		Now:               now,
		OrphanGracePeriod: time.Hour,
		ArchivedRetention: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if result.UnregisteredRemoved != 1 || result.MissingRootRemoved != 1 || result.ArchivedRemoved != 1 {
		t.Fatalf("unexpected prune result: %+v", result)
	}
	if result.OrphanLocksRemoved != 1 {
		t.Fatalf("orphan locks removed = %d, want 1", result.OrphanLocksRemoved)
	}

	for _, ws := range []*Workspace{orphanOld, missing, archived} {
		if _, err := store.Load(ws.ID()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Load(%s) error = %v, want not exist", ws.Name, err)
		}
	}
	for _, ws := range []*Workspace{keep, orphanFresh} {
		if _, err := store.Load(ws.ID()); err != nil {
			t.Fatalf("Load(%s): %v", ws.Name, err)
		}
	}
	if _, err := os.Stat(orphanRoot); err != nil {
		t.Fatalf("pruning metadata removed workspace files: %v", err)
	}
	if _, err := os.Stat(store.workspaceLockPath(orphanLockID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan lock still exists, stat err = %v", err)
	}
}

func TestWorkspaceStoreDeleteByRepoLeavesWorkspaceRoots(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	dropRepo := filepath.Join(root, "drop")
	keepRepo := filepath.Join(root, "keep")
	dropRootA := filepath.Join(root, "workspaces", "drop", "a")
	dropRootB := filepath.Join(root, "workspaces", "drop", "b")
	for _, path := range []string{dropRootA, dropRootB} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dropA := NewWorkspace("a", "a", "main", dropRepo, dropRootA)
	dropB := NewWorkspace("b", "b", "main", dropRepo, dropRootB)
	keep := NewWorkspace("keep", "keep", "main", keepRepo, keepRepo)
	for _, ws := range []*Workspace{dropA, dropB, keep} {
		if err := store.Save(ws); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := store.DeleteByRepo(dropRepo)
	if err != nil {
		t.Fatalf("DeleteByRepo: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed IDs = %v, want 2", removed)
	}
	if _, err := store.Load(keep.ID()); err != nil {
		t.Fatalf("unrelated metadata removed: %v", err)
	}
	for _, path := range []string{dropRootA, dropRootB} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("workspace root %s was removed: %v", path, err)
		}
	}
}

func TestWorkspaceStoreDeleteByRepoReturnsLegacyAndCanonicalSessionIDs(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	repo := filepath.Join(root, "repo")
	workspaceRoot := filepath.Join(root, "workspaces", "repo", "feature")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace("feature", "feature", "main", repo, workspaceRoot)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	canonicalID := ws.ID()
	legacyID := WorkspaceID("legacy-record-id")
	if err := os.Rename(
		filepath.Join(store.root, string(canonicalID)),
		filepath.Join(store.root, string(legacyID)),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.workspaceLockPath(canonicalID), store.workspaceLockPath(legacyID)); err != nil {
		t.Fatal(err)
	}

	removed, err := store.DeleteByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := map[WorkspaceID]bool{legacyID: true, canonicalID: true}
	if len(removed) != len(want) {
		t.Fatalf("removed IDs = %v, want legacy and canonical IDs", removed)
	}
	for _, id := range removed {
		if !want[id] {
			t.Fatalf("unexpected removed ID %s in %v", id, removed)
		}
	}
	if _, err := os.Stat(workspaceRoot); err != nil {
		t.Fatalf("DeleteByRepo removed workspace files: %v", err)
	}
}
