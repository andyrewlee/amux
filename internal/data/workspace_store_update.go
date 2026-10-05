package data

import (
	"errors"
	"fmt"
	"io/fs"
)

// Update runs fn as a read-modify-write transaction under the workspace's
// flock: validate, lock, fresh load, mutate, atomic write — one critical
// section. It exists because load+Save pairs are not composable: two writers
// each holding a whole-record snapshot can each save the other's changes
// away (a delayed tab-state write resurrecting a stale Env, a rename
// reverting a script edit). With Update the field under edit is the only
// thing that changes on disk.
//
// fn sees the record with defaults applied, exactly as Load returns it. It
// must be pure — no store calls (the flock is per-open-fd and not
// reentrant), no I/O, no other synchronization — and it must not change the
// record's identity (Repo/Root) or schema version; both are verified after
// it returns and abort the write. A false changed result performs no disk
// write and emits no watch event. Update never creates a missing record and
// never follows a callback-selected new ID — creation and migration are
// Save's job.
func (s *WorkspaceStore) Update(id WorkspaceID, fn func(ws *Workspace) (changed bool, err error)) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("update callback is required")
	}
	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)

	// Fresh load inside the lock: the callback mutates the freshest committed
	// record, not whatever snapshot the caller was holding. Defaults applied,
	// matching what setter callers saw through Load; the future-schema
	// refusal is preserved.
	ws, err := s.load(id, true)
	if err != nil {
		return fmt.Errorf("update workspace %s: %w", id, err)
	}
	ws.storeID = id
	repo, root := ws.Repo, ws.Root

	changed, err := fn(ws)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if ws.Repo != repo || ws.Root != root {
		return fmt.Errorf("update workspace %s: callback changed the record's identity", id)
	}
	if ws.Version > workspaceFileVersion {
		return fmt.Errorf("update workspace %s: callback bumped schema to v%d", id, ws.Version)
	}
	if err := validateWorkspaceForSave(ws); err != nil {
		return err
	}
	return s.saveWorkspaceLocked(id, ws)
}

// SaveIfAbsent writes ws only when no stored record matches its identity —
// the create half of discovery's check-then-act, closed so the recheck runs
// inside the candidate-ID flock instead of trusting the caller's unlocked
// lookup. A record committed between that lookup and this call is observed
// here rather than overwritten by ws's sparse fields: the winner's record
// comes back (stored, created=false) for the caller to merge or adopt.
// Corrupt, unreadable, or newer-schema existing bytes are existing state —
// refused, never clobbered.
func (s *WorkspaceStore) SaveIfAbsent(ws *Workspace) (stored *Workspace, created bool, err error) {
	if err := validateWorkspaceForSave(ws); err != nil {
		return nil, false, err
	}
	id := ws.ID()
	if err := validateWorkspaceID(id); err != nil {
		return nil, false, err
	}
	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return nil, false, err
	}
	defer unlockRegistryFiles(lockFiles)

	// Recheck under the flock: first the canonical key, then the
	// normalization-drift fallback in case the concurrent create landed
	// under a sibling key for the same repo+root.
	if existing, loadErr := s.load(id, false); loadErr == nil {
		return existing, false, nil
	} else if !errors.Is(loadErr, fs.ErrNotExist) {
		return nil, false, loadErr
	}
	existing, foundID, err := s.findStoredWorkspace(ws.Repo, ws.Root)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		existing.storeID = foundID
		return existing, false, nil
	}

	if ws.Created.IsZero() {
		ws.Created = s.clock()
	}
	s.applyWorkspaceDefaults(ws)
	if err := s.saveWorkspaceLocked(id, ws); err != nil {
		return nil, false, err
	}
	ws.storeID = id
	return ws, true, nil
}
