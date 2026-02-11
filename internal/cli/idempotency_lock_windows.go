//go:build windows

package cli

import (
	"os"
	"path/filepath"
)

func lockIdempotencyFile(lockPath string, _ bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
}

func unlockIdempotencyFile(file *os.File) {
	if file == nil {
		return
	}
	_ = file.Close()
}
