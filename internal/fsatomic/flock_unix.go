//go:build !windows

package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile takes an advisory flock on lockPath — the sibling "<file>.lock"
// rendezvous convention amux's singleton metadata stores use — and returns
// the open lockfile whose lifetime pins the lock. shared selects LOCK_SH for
// read transactions, LOCK_EX for write transactions. Callers hold the file
// across their whole load→mutate→write so a second process cannot slip a
// lost update into the read half; UnlockFile releases it.
//
// flock binds the lock to the open-file description, so a deleted-and-
// recreated rendezvous would let a waiter hold a dead inode while a new
// acquirer locks the live path. The current-check after flock detects the
// replacement and retries until the locked fd matches the path on disk.
func LockFile(lockPath string, shared bool) (*os.File, error) {
	if err := mkdirAllPrivate(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	flag := syscall.LOCK_EX
	if shared {
		flag = syscall.LOCK_SH
	}

	for {
		root, file, lockName, err := openLockRootRetry(lockPath)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(file.Fd()), flag); err != nil {
			_ = root.Close()
			_ = file.Close()
			return nil, err
		}
		current, err := lockFileCurrent(file, root, lockName)
		closeErr := root.Close()
		if err != nil {
			UnlockFile(file)
			return nil, err
		}
		if closeErr != nil {
			UnlockFile(file)
			return nil, closeErr
		}
		if current {
			return file, nil
		}
		UnlockFile(file)
	}
}

// UnlockFile releases the lock and closes the lockfile.
func UnlockFile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func lockFileCurrent(file *os.File, root *os.Root, lockName string) (bool, error) {
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
