package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/fsatomic"
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
// begin. The marker is written crash-safely (temp + fsync + atomic rename) via
// fsatomic, so an interrupted delete can never leave a torn marker that startup
// recovery would reject.
func (s *WorkspaceStore) MarkDeleting(id WorkspaceID) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	dir := filepath.Join(s.root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fsatomic.WriteFile(s.deletingMarkerPath(id), []byte("1"), 0o644)
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
