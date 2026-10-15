package data

import (
	"errors"
	"io/fs"
	"time"
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
		// The lookup ran unlocked: a concurrent create can commit between
		// here and the save. SaveIfAbsent rechecks inside the record flock —
		// a winner gets merged like any existing record, never clobbered by
		// the sparse discovered snapshot.
		if upsertAbsentProbe != nil {
			upsertAbsentProbe()
		}
		stored, created, err := s.SaveIfAbsent(discovered)
		if err != nil {
			return err
		}
		if created {
			return nil
		}
		return s.mergeDiscoveryLocked(discovered, stored.storeID)
	}

	return s.mergeDiscoveryLocked(discovered, storedID)
}

// upsertAbsentProbe fires inside UpsertFromDiscovery's missing-record branch
// between the unlocked lookup and the create-if-absent save — the test seam
// that lands a concurrent write exactly in the lost-update window.
var upsertAbsentProbe func()

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
	defer unlockRegistryFiles(lockFiles)

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
			discovered.Created = s.clock()
		}
		s.applyWorkspaceDefaults(discovered)
		if discovered.ID() == storedID {
			if err := s.saveWorkspaceLocked(storedID, discovered); err != nil {
				return err
			}
			discovered.storeID = storedID
			return nil
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
	// A discovered worktree also ends any shelf: the path exists again, so the
	// record is live regardless of how it was archived.
	merged.Shelved = false
	s.applyWorkspaceDefaults(&merged)

	// Identity is pinned by storedID: merged inherited stored.storeID (set
	// above), so merged.ID() can only return storedID — the record always
	// saves in place under the held flock, eliminating the lost-update
	// window. A Repo/Root change updates the record's fields, never its key.
	if err := s.saveWorkspaceLocked(storedID, &merged); err != nil {
		return err
	}
	// Reflect the live key onto the caller's object — a caller that keeps
	// using `discovered` after the merge (env building, port reservation)
	// otherwise sees a workspace with no stored identity at all.
	discovered.storeID = storedID
	return nil
}
