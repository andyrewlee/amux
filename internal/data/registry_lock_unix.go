//go:build !windows

package data

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	flag := syscall.LOCK_EX
	if shared {
		flag = syscall.LOCK_SH
	}
	if err := syscall.Flock(int(file.Fd()), flag); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func unlockRegistryFile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
