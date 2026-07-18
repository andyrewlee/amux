package data

import "fmt"

// SetPinned updates a workspace's pinned flag and persists it. Like SetEnv
// and Rename (the Tier-1 single-field-update shape), it loads the workspace
// fresh from disk before mutating and saving in place, so a caller holding a
// possibly-stale in-memory Workspace cannot clobber a field another in-flight
// operation changed concurrently.
func (s *WorkspaceStore) SetPinned(id WorkspaceID, pinned bool) error {
	ws, err := s.Load(id)
	if err != nil {
		return fmt.Errorf("set pinned for workspace %s: %w", id, err)
	}
	if ws.Pinned == pinned {
		return nil
	}
	ws.Pinned = pinned
	if err := s.Save(ws); err != nil {
		return fmt.Errorf("set pinned for workspace %s: %w", id, err)
	}
	return nil
}
