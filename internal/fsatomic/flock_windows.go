//go:build windows

package fsatomic

import (
	"os"
	"path/filepath"
)

// LockFile is the Windows form of the unix flock helper: best-effort. We open
// the lockfile to signal intent, but we don't enforce exclusive locking
// across processes here.
func LockFile(lockPath string, shared bool) (*os.File, error) {
	if err := mkdirAllPrivate(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	root, file, _, err := openLockRootRetry(lockPath)
	if err != nil {
		return nil, err
	}
	if err := root.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// UnlockFile releases the lockfile.
func UnlockFile(file *os.File) {
	if file == nil {
		return
	}
	_ = file.Close()
}
