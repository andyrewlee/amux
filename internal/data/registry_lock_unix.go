//go:build !windows

package data

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	flag := syscall.LOCK_EX
	if shared {
		flag = syscall.LOCK_SH
	}

	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(file.Fd()), flag); err != nil {
			_ = file.Close()
			return nil, err
		}
		current, err := registryLockFileCurrent(file, lockPath)
		if err != nil {
			unlockRegistryFile(file)
			return nil, err
		}
		if current {
			return file, nil
		}
		unlockRegistryFile(file)
	}
}

func unlockRegistryFile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func registryLockFileCurrent(file *os.File, lockPath string) (bool, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	pathInfo, err := os.Stat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fileStat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return true, nil
	}
	pathStat, ok := pathInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return true, nil
	}
	return fileStat.Dev == pathStat.Dev && fileStat.Ino == pathStat.Ino, nil
}
