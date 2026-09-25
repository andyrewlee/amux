package data

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneStaleBackupModTimeFallback: when the primary metadata file is gone
// but its .bak survives, the .bak modtime drives the grace check — an old
// backup is pruned, a fresh one retained. Pinning this matters because the
// fallback is the only signal a crashed write leaves behind.
func TestPruneStaleBackupModTimeFallback(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	managedRoot := filepath.Join(root, "workspaces")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(managedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}

	old := NewWorkspace("old", "old", "main", repo, filepath.Join(managedRoot, "repo", "old"))
	fresh := NewWorkspace("fresh", "fresh", "main", repo, filepath.Join(managedRoot, "repo", "fresh"))
	now := time.Now()
	for _, ws := range []*Workspace{old, fresh} {
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%s): %v", ws.Name, err)
		}
		// Leave only the backup: primary deleted, .bak = same bytes.
		primary, err := os.ReadFile(store.workspacePath(ws.ID()))
		if err != nil {
			t.Fatalf("read primary: %v", err)
		}
		if err := os.WriteFile(store.workspaceBackupPath(ws.ID()), primary, 0o600); err != nil {
			t.Fatalf("write backup: %v", err)
		}
		if err := os.Remove(store.workspacePath(ws.ID())); err != nil {
			t.Fatalf("remove primary: %v", err)
		}
	}
	oldTime := now.Add(-2 * time.Hour)
	freshTime := now.Add(-30 * time.Minute)
	if err := os.Chtimes(store.workspaceBackupPath(old.ID()), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(store.workspaceBackupPath(fresh.ID()), freshTime, freshTime); err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos:   []string{repo},
		ManagedRoot:       managedRoot,
		Now:               now,
		OrphanGracePeriod: time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if result.MissingRootRemoved != 1 {
		t.Fatalf("MissingRootRemoved = %d, want 1 (old backup)", result.MissingRootRemoved)
	}
	if _, err := store.Load(old.ID()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old record survived, load err = %v", err)
	}
	if _, err := store.Load(fresh.ID()); err != nil {
		t.Fatalf("fresh-backup record pruned despite grace period: %v", err)
	}
}

// TestPruneStaleRetainsUnreadableMetadata: a record whose bytes do not parse
// is retained and reported — without a trustworthy repo/root the store cannot
// prove it stale, so it must not silently drop it.
func TestPruneStaleRetainsUnreadableMetadata(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace("corrupt", "corrupt", "main", repo, filepath.Join(root, "workspaces", "repo", "corrupt"))
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.workspacePath(ws.ID()), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos: []string{repo},
		ManagedRoot:     filepath.Join(root, "workspaces"),
		Now:             time.Now(),
	})
	if err == nil {
		t.Fatal("corrupt metadata produced no error — it must surface, not vanish")
	}
	if result.MetadataRemoved() != 0 {
		t.Fatalf("pruned %+v on an unreadable record", result)
	}
	if _, err := os.Stat(filepath.Join(store.root, string(ws.ID()))); err != nil {
		t.Fatalf("unreadable metadata dir removed: %v", err)
	}
}

// TestPruneStalePrimaryCheckoutKeepsMissingRoot pins the IsPrimaryCheckout
// gate inside missing_root: a primary checkout's root can vanish (external
// cleanup) while its metadata is still meaningful. The sibling record — same
// repo, non-primary root inside the managed root — prunes normally.
func TestPruneStalePrimaryCheckoutKeepsMissingRoot(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	managedRoot := filepath.Join(root, "workspaces")
	// The repo lives INSIDE the managed root so withinManagedRoot holds for
	// both records — isolating IsPrimaryCheckout as the only difference.
	repo := filepath.Join(managedRoot, "repo")
	primaryRoot := filepath.Join(repo, "primary")
	subRoot := filepath.Join(repo, "sub")
	for _, path := range []string{primaryRoot, subRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	primary := NewWorkspace("primary", "primary", "main", primaryRoot, primaryRoot)
	sub := NewWorkspace("sub", "sub", "main", primaryRoot, subRoot)
	for _, ws := range []*Workspace{primary, sub} {
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%s): %v", ws.Name, err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, ws := range []*Workspace{primary, sub} {
		if err := os.Chtimes(store.workspacePath(ws.ID()), old, old); err != nil {
			t.Fatal(err)
		}
	}
	// Remove both roots (and the repo itself — the registered-repos lookup
	// matches by canonical string, not liveness).
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos:   []string{primaryRoot},
		ManagedRoot:       managedRoot,
		Now:               time.Now(),
		OrphanGracePeriod: time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if result.MissingRootRemoved != 1 {
		t.Fatalf("MissingRootRemoved = %d, want 1 (only the non-primary record)", result.MissingRootRemoved)
	}
	if _, err := store.Load(primary.ID()); err != nil {
		t.Fatalf("primary-checkout metadata pruned on missing root: %v", err)
	}
	if _, err := store.Load(sub.ID()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-primary missing-root record retained, err = %v", err)
	}
}

// TestPruneStaleOutsideManagedRootRetained covers the withinManagedRoot
// exclusion: a missing root outside the managed tree (an external checkout the
// user deleted by hand) must not be reaped as amux-managed state.
func TestPruneStaleOutsideManagedRootRetained(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	managedRoot := filepath.Join(root, "workspaces")
	repo := filepath.Join(root, "repo")
	outsideRoot := filepath.Join(root, "external-checkout")
	for _, path := range []string{managedRoot, repo, outsideRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	outside := NewWorkspace("outside", "outside", "main", repo, outsideRoot)
	if err := store.Save(outside); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(store.workspacePath(outside.ID()), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(outsideRoot); err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos:   []string{repo},
		ManagedRoot:       managedRoot,
		Now:               time.Now(),
		OrphanGracePeriod: time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if result.MissingRootRemoved != 0 {
		t.Fatalf("MissingRootRemoved = %d, want 0 (outside managed root)", result.MissingRootRemoved)
	}
	if _, err := store.Load(outside.ID()); err != nil {
		t.Fatalf("outside-managed-root record pruned: %v", err)
	}
}

// TestPruneOrphanLocksBranches covers pruneOrphanLocks: a lock whose metadata
// exists stays, an invalid lock filename is skipped in place, a lock whose
// path can't even be opened accumulates an error, and a true orphan is removed.
func TestPruneOrphanLocksBranches(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(filepath.Join(root, "metadata"))
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}

	wsRoot := filepath.Join(root, "workspaces", "repo", "alive")
	if err := os.MkdirAll(wsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace("alive", "alive", "main", repo, wsRoot)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}

	// Lock for a live record — pruneOrphanLocks must leave it.
	locks, err := store.lockWorkspaceIDs(ws.ID())
	if err != nil {
		t.Fatalf("lockWorkspaceIDs: %v", err)
	}
	unlockRegistryFiles(locks)

	// Invalid filename (".." rejected by validateWorkspaceID) — skipped, kept.
	invalidLock := filepath.Join(store.root, "bad..name.lock")
	if err := os.WriteFile(invalidLock, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// A lock path that resolves to a directory via symlink: OpenFile(O_RDWR)
	// fails with EISDIR — exercises the lockErr accumulation branch.
	dirLock := filepath.Join(store.root, "dirlock.lock")
	if err := os.Symlink(store.root, dirLock); err != nil {
		t.Fatal(err)
	}
	// A true orphan — removed.
	orphan := WorkspaceID("pure-orphan")
	locks, err = store.lockWorkspaceIDs(orphan)
	if err != nil {
		t.Fatal(err)
	}
	unlockRegistryFiles(locks)

	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos: []string{repo},
		ManagedRoot:     filepath.Join(root, "workspaces"),
		Now:             time.Now(),
	})
	if err == nil {
		t.Fatal("expected the unopenable lock path to accumulate an error")
	}
	if result.OrphanLocksRemoved != 1 {
		t.Fatalf("OrphanLocksRemoved = %d, want 1", result.OrphanLocksRemoved)
	}
	if _, err := os.Stat(store.workspaceLockPath(ws.ID())); err != nil {
		t.Fatalf("live record's lock file removed: %v", err)
	}
	if _, err := os.Stat(invalidLock); err != nil {
		t.Fatalf("invalid-name lock removed: %v", err)
	}
	if _, err := os.Lstat(dirLock); err != nil {
		t.Fatalf("unopenable lock path removed: %v", err)
	}
	if _, err := os.Stat(store.workspaceLockPath(orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan lock still exists, stat err = %v", err)
	}
}
