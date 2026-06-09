package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// deletingMarkerName is the per-workspace delete tombstone. It lives inside the
// workspace's metadata directory so a successful Delete (which removes that whole
// directory) clears it automatically; only an interrupted delete leaves it for
// startup recovery to finish.
const deletingMarkerName = ".deleting"

func (s *WorkspaceStore) deletingMarkerPath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id), deletingMarkerName)
}

// MarkDeleting writes a durable tombstone for id before destructive delete steps
// begin. The marker is written atomically (temp file + rename).
func (s *WorkspaceStore) MarkDeleting(id WorkspaceID) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	dir := filepath.Join(s.root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	marker := s.deletingMarkerPath(id)
	tmp := marker + ".tmp"
	if err := os.WriteFile(tmp, []byte("1"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, marker); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// IsDeleting reports whether a delete tombstone exists for id.
func (s *WorkspaceStore) IsDeleting(id WorkspaceID) bool {
	if validateWorkspaceID(id) != nil {
		return false
	}
	_, err := os.Stat(s.deletingMarkerPath(id))
	return err == nil
}

// ClearDeleting removes the delete tombstone for id. A missing marker is not an
// error (already cleared, or the delete already removed the whole directory).
func (s *WorkspaceStore) ClearDeleting(id WorkspaceID) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	if err := os.Remove(s.deletingMarkerPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
