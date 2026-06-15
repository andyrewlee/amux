package data

import (
	"errors"
	"io/fs"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

// UpsertFromDiscovery merges a discovered workspace into the store.
// Store metadata wins; discovery updates Repo/Root/Branch (and Name if empty).
// Archived state is cleared on discovery.
//
// The merge runs as an atomic read-modify-write: the discovered fields are
// reconciled against the stored metadata while the workspace flock is held
// across the reload+merge+save, mirroring Update(id, fn). Holding the lock over
// the whole sequence prevents lost updates when two amux processes rescan the
// same repo concurrently — without it, each would load->merge->save in turn and
// silently clobber the other's fields (e.g. OpenTabs, Archived).
func (s *WorkspaceStore) UpsertFromDiscovery(discovered *Workspace) error {
	if discovered == nil {
		return nil
	}

	stored, storedID, err := s.findStoredWorkspace(discovered.Repo, discovered.Root)
	if err != nil {
		return err
	}

	if stored == nil {
		if discovered.Created.IsZero() {
			discovered.Created = time.Now()
		}
		s.applyWorkspaceDefaults(discovered)
		return s.Save(discovered)
	}

	return s.mergeDiscoveryLocked(discovered, storedID)
}

// mergeDiscoveryLocked re-loads the stored workspace under its flock, merges the
// discovered deltas, and saves the result — all within a single locked critical
// section so concurrent rescans cannot drop each other's fields. The initial
// lookup (findStoredWorkspace) selected storedID; the reload here observes any
// fields a racing writer committed in the meantime, then this call wins or loses
// the lock atomically rather than racing on a stale in-memory copy.
func (s *WorkspaceStore) mergeDiscoveryLocked(discovered *Workspace, storedID WorkspaceID) error {
	lockFiles, err := s.lockWorkspaceIDs(storedID)
	if err != nil {
		return err
	}
	// Deferred via closure (not defer unlockRegistryFiles(lockFiles)) so that the
	// rename path below can release the flock early and clear lockFiles, leaving
	// nothing for this deferred call to double-unlock.
	defer func() { unlockRegistryFiles(lockFiles) }()

	// Reload under the lock so the merge applies to the freshest committed
	// metadata, not the copy read before the lock was acquired. applyDefaults=false
	// keeps empty fields visible so the merge precedence below behaves as before.
	stored, err := s.load(storedID, false)
	if err != nil {
		// The metadata vanished between lookup and lock (e.g. a concurrent delete).
		// Fall back to persisting the discovery as a fresh record.
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if discovered.Created.IsZero() {
			discovered.Created = time.Now()
		}
		s.applyWorkspaceDefaults(discovered)
		if discovered.ID() == storedID {
			return s.saveWorkspaceLocked(storedID, discovered)
		}
		return s.Save(discovered)
	}
	stored.storeID = storedID

	merged := *stored
	merged.Repo = discovered.Repo
	merged.Root = discovered.Root
	merged.Branch = discovered.Branch
	if merged.Name == "" {
		merged.Name = discovered.Name
	}
	if merged.Assistant == "" {
		merged.Assistant = discovered.Assistant
	}
	if merged.Created.IsZero() && !discovered.Created.IsZero() {
		merged.Created = discovered.Created
	}
	merged.Archived = false
	merged.ArchivedAt = time.Time{}
	s.applyWorkspaceDefaults(&merged)

	newID := merged.ID()
	if newID == storedID {
		// Common case: discovery did not change Repo/Root, so the canonical ID is
		// unchanged. Write in place while still holding the flock — the entire
		// load-merge-save is atomic, eliminating the lost-update window.
		return s.saveWorkspaceLocked(storedID, &merged)
	}

	// Rename case: Repo/Root changed, so the merged record lives under a new ID.
	// Save acquires both id and oldID flocks (via storeID); release the storedID
	// flock first to avoid re-locking the same file from this process (flock is
	// per-open-fd and not reentrant), then save and clean up the old record.
	merged.storeID = storedID
	unlockRegistryFiles(lockFiles)
	lockFiles = nil
	if err := s.Save(&merged); err != nil {
		return err
	}
	if storedID != "" && storedID != newID {
		if err := s.Delete(storedID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", storedID, err)
		}
	}
	return nil
}
