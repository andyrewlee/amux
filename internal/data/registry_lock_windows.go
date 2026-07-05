//go:build windows

package data

import (
	"os"
	"path/filepath"
)

// Best-effort lock on Windows. We open the lockfile to signal intent, but
// we don't enforce exclusive locking across processes here.
func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	if err := mkdirAllPrivate(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	root, file, _, err := openRegistryLockRoot(lockPath)
	if err != nil {
		return nil, err
	}
	if err := root.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func unlockRegistryFile(file *os.File) {
	if file == nil {
		return
	}
	_ = file.Close()
}
