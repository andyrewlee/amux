package sandbox

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func applyDownloadedWorkspace(srcRoot, dstRoot string, ignorePatterns []string) error {
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return err
	}
	if err := pruneMissingWorkspacePaths(dstRoot, srcRoot, ignorePatterns); err != nil {
		return err
	}
	return copyWorkspaceTree(srcRoot, dstRoot, ignorePatterns)
}

func pruneMissingWorkspacePaths(dstRoot, srcRoot string, ignorePatterns []string) error {
	entries, err := os.ReadDir(dstRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := pruneMissingWorkspacePath(entry.Name(), dstRoot, srcRoot, ignorePatterns); err != nil {
			return err
		}
	}
	return nil
}

func pruneMissingWorkspacePath(relPath, dstRoot, srcRoot string, ignorePatterns []string) error {
	if relPath == "" || relPath == "." {
		return nil
	}
	if shouldIgnoreFile(relPath, ignorePatterns) {
		return nil
	}

	dstPath := filepath.Join(dstRoot, relPath)
	srcPath := filepath.Join(srcRoot, relPath)

	srcInfo, err := os.Lstat(srcPath)
	if errors.Is(err, os.ErrNotExist) {
		dstInfo, dstErr := os.Lstat(dstPath)
		if errors.Is(dstErr, os.ErrNotExist) {
			return nil
		}
		if dstErr != nil {
			return dstErr
		}

		// Preserve ignored descendants under deleted parents by pruning
		// recursively instead of deleting the whole subtree at once.
		if dstInfo.IsDir() && dstInfo.Mode()&os.ModeSymlink == 0 {
			children, readErr := os.ReadDir(dstPath)
			if readErr != nil {
				return readErr
			}
			for _, child := range children {
				childRelPath := filepath.Join(relPath, child.Name())
				if pruneErr := pruneMissingWorkspacePath(childRelPath, dstRoot, srcRoot, ignorePatterns); pruneErr != nil {
					return pruneErr
				}
			}

			remaining, readRemainingErr := os.ReadDir(dstPath)
			if readRemainingErr != nil {
				return readRemainingErr
			}
			if len(remaining) == 0 {
				return os.Remove(dstPath)
			}
			return nil
		}

		return os.RemoveAll(dstPath)
	}
	if err != nil {
		return err
	}
	dstInfo, err := os.Lstat(dstPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	srcIsDir := srcInfo.IsDir()
	dstIsDir := dstInfo.IsDir()
	srcIsSymlink := srcInfo.Mode()&os.ModeSymlink != 0
	dstIsSymlink := dstInfo.Mode()&os.ModeSymlink != 0

	// If file types diverge, remove the destination and let copyWorkspaceTree recreate it.
	if srcIsDir != dstIsDir || srcIsSymlink != dstIsSymlink {
		return os.RemoveAll(dstPath)
	}

	if !dstIsDir {
		return nil
	}

	children, err := os.ReadDir(dstPath)
	if err != nil {
		return err
	}
	for _, child := range children {
		childRelPath := filepath.Join(relPath, child.Name())
		if err := pruneMissingWorkspacePath(childRelPath, dstRoot, srcRoot, ignorePatterns); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkspaceTree(srcRoot, dstRoot string, ignorePatterns []string) error {
	return filepath.WalkDir(srcRoot, func(srcPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(srcRoot, srcPath)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		if shouldIgnoreFile(relPath, ignorePatterns) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dstRoot, relPath)
		srcInfo, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}

		switch {
		case srcInfo.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			if err := os.RemoveAll(dstPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return os.Symlink(linkTarget, dstPath)
		case entry.IsDir():
			return os.MkdirAll(dstPath, srcInfo.Mode().Perm())
		case srcInfo.Mode().IsRegular():
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return err
			}
			if err := copyFileWithMode(srcPath, dstPath, srcInfo.Mode().Perm()); err != nil {
				return err
			}
			return nil
		default:
			// Ignore unsupported file types.
			return nil
		}
	})
}

func copyFileWithMode(srcPath, dstPath string, mode os.FileMode) error {
	if dstInfo, err := os.Lstat(dstPath); err == nil {
		if dstInfo.IsDir() || dstInfo.Mode()&os.ModeSymlink != 0 {
			if err := os.RemoveAll(dstPath); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	srcFile, err := openReadableTempFile(srcPath, mode)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}
	return dstFile.Close()
}

// openReadableTempFile opens a file from the extracted snapshot tree.
// Some files may preserve restrictive source modes (e.g. 000), so we
// temporarily add owner-read permission in the temp tree when needed.
func openReadableTempFile(path string, originalMode os.FileMode) (*os.File, error) {
	file, err := os.Open(path)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrPermission) && !os.IsPermission(err) {
		return nil, err
	}
	if chmodErr := os.Chmod(path, originalMode|0o400); chmodErr != nil {
		return nil, err
	}
	return os.Open(path)
}
