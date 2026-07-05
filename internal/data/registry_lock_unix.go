//go:build !windows

package data

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockRegistryFile(lockPath string, shared bool) (*os.File, error) {
	if err := mkdirAllPrivate(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	flag := syscall.LOCK_EX
	if shared {
		flag = syscall.LOCK_SH
	}

	for {
		root, file, lockName, err := openRegistryLockRoot(lockPath)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(file.Fd()), flag); err != nil {
			_ = root.Close()
			_ = file.Close()
			return nil, err
		}
		current, err := registryLockFileCurrent(file, root, lockName)
		closeErr := root.Close()
		if err != nil {
			unlockRegistryFile(file)
			return nil, err
		}
		if closeErr != nil {
			unlockRegistryFile(file)
			return nil, closeErr
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

func registryLockFileCurrent(file *os.File, root *os.Root, lockName string) (bool, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	pathInfo, err := root.Stat(lockName)
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
