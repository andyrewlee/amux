//go:build windows

package data

import (
	"os"
	"path/filepath"
)

// Best-effort lock on Windows. We open the lockfile to signal intent, but
// we don't enforce exclusive locking across processes here.
func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
}

func unlockRegistryFile(file *os.File) {
	if file == nil {
		return
	}
	_ = file.Close()
}
