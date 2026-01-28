package config

import (
	"io"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/logging"
)

// MigrateResult tracks what was migrated
type MigrateResult struct {
	MigratedWorkspacesRoot bool
	MigratedMetadataRoot   bool
	Error                  error
}

// HasMigrations returns true if any migrations were performed
func (r *MigrateResult) HasMigrations() bool {
	return r.MigratedWorkspacesRoot || r.MigratedMetadataRoot
}

// RunMigrations performs idempotent migration from legacy paths.
// It migrates from ~/.amux/worktrees to ~/.amux/workspaces and
// from ~/.amux/worktrees-metadata to ~/.amux/workspaces-metadata.
// Migration only occurs if the legacy path exists and the new path doesn't.
// Legacy directories are never deleted.
func (p *Paths) RunMigrations() *MigrateResult {
	result := &MigrateResult{}

	// Derive legacy paths from Home
	legacyWorkspacesRoot := filepath.Join(p.Home, "worktrees")
	legacyMetadataRoot := filepath.Join(p.Home, "worktrees-metadata")

	// Migrate workspaces root
	migrated, err := copyDirIfNeeded(legacyWorkspacesRoot, p.WorkspacesRoot)
	if err != nil {
		result.Error = err
		return result
	}
	result.MigratedWorkspacesRoot = migrated
	if migrated {
		logging.Info("Migrated %s -> %s", legacyWorkspacesRoot, p.WorkspacesRoot)
	}

	// Migrate metadata root
	migrated, err = copyDirIfNeeded(legacyMetadataRoot, p.MetadataRoot)
	if err != nil {
		result.Error = err
		return result
	}
	result.MigratedMetadataRoot = migrated
	if migrated {
		logging.Info("Migrated %s -> %s", legacyMetadataRoot, p.MetadataRoot)
	}

	return result
}

// copyDirIfNeeded copies src to dst only if src exists and dst doesn't.
// Returns true if a copy was performed, false otherwise.
func copyDirIfNeeded(src, dst string) (bool, error) {
	// Check if source exists
	srcInfo, err := os.Stat(src)
	if os.IsNotExist(err) {
		// Source doesn't exist, nothing to migrate
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !srcInfo.IsDir() {
		// Source is not a directory, skip
		return false, nil
	}

	// Check if destination already exists
	_, err = os.Stat(dst)
	if err == nil {
		// Destination exists, don't overwrite
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}

	// Perform the copy
	if err := copyDir(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return false, err
	}

	return true, nil
}

// copyDir recursively copies a directory tree
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory with same permissions
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(linkTarget, dstPath); err != nil {
				return err
			}
			continue
		}

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file preserving permissions
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}
