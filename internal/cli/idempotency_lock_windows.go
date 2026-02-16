//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func lockIdempotencyFile(lockPath string, shared bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	var flags uint32 = windows.LOCKFILE_FAIL_IMMEDIATELY
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	ol := new(windows.Overlapped)
	// Lock the first byte range (entire file). Using maxLen ensures the lock
	// covers any file size, matching the Unix flock() semantics.
	const maxLen = ^uint32(0)
	err = windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, maxLen, maxLen, ol)
	if err != nil {
		// FAIL_IMMEDIATELY may return ERROR_LOCK_VIOLATION; fall back to blocking.
		flags &^= windows.LOCKFILE_FAIL_IMMEDIATELY
		err = windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, maxLen, maxLen, ol)
	}
	if err != nil {
		_ = file.Close()
		return nil, &os.PathError{Op: "lock", Path: lockPath, Err: err}
	}
	// Prevent ol from being GC'd before LockFileEx returns.
	_ = unsafe.Pointer(ol)
	return file, nil
}

func unlockIdempotencyFile(file *os.File) {
	if file == nil {
		return
	}
	ol := new(windows.Overlapped)
	const maxLen = ^uint32(0)
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, maxLen, maxLen, ol)
	_ = file.Close()
}
