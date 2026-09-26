package data

import (
	"errors"
	"fmt"
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
