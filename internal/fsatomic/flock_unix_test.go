//go:build !windows

package fsatomic

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLockFileRetriesWhenWaiterAcquiresUnlinkedInode(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "workspace.lock")
	held, err := LockFile(lockPath, false)
	if err != nil {
		t.Fatalf("initial LockFile() error = %v", err)
	}

	waiter := make(chan *os.File, 1)
	waitErr := make(chan error, 1)
	go func() {
		file, lockErr := LockFile(lockPath, false)
		if lockErr != nil {
			waitErr <- lockErr
			return
		}
		waiter <- file
	}()
	time.Sleep(100 * time.Millisecond)

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove lock path: %v", err)
	}
	replacement, err := LockFile(lockPath, false)
	if err != nil {
		t.Fatalf("replacement LockFile() error = %v", err)
	}
	UnlockFile(held)

	select {
	case err := <-waitErr:
		UnlockFile(replacement)
		t.Fatalf("waiter LockFile() error = %v", err)
	case file := <-waiter:
		UnlockFile(file)
		UnlockFile(replacement)
		t.Fatal("waiter acquired the unlinked inode while the replacement lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	UnlockFile(replacement)
	select {
	case err := <-waitErr:
		t.Fatalf("waiter LockFile() error = %v", err)
	case file := <-waiter:
		UnlockFile(file)
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not acquire the replacement lock after it was released")
	}
}

// TestLockFileRetriesTransientENOENT pins the open-side retry: the darwin
// openat-under-os.Root race can report ENOENT on a live directory, and the
// lock acquisition must ride out a short burst rather than handing the
// caller a spurious failure (a wedged store ID is how the durable port path
// used to surface this). The seam forces the burst; the real open then
// succeeds.
func TestLockFileRetriesTransientENOENT(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "workspace.lock")
	var calls atomic.Int32
	prev := openLockRootFn
	openLockRootFn = func(path string) (*os.Root, *os.File, string, error) {
		if calls.Add(1) <= 3 {
			return nil, nil, "", fs.ErrNotExist
		}
		return prev(path)
	}
	defer func() { openLockRootFn = prev }()

	file, err := LockFile(lockPath, false)
	if err != nil {
		t.Fatalf("LockFile() error = %v, want retry to converge", err)
	}
	UnlockFile(file)
	if got := calls.Load(); got != 4 {
		t.Fatalf("open calls = %d, want 4 (3 forced ENOENT + success)", got)
	}
}

// TestLockFileENOENTBudgetExhausted proves a persistently missing directory
// still fails closed after the retry budget runs out — the retry only rides
// out the transient race, it does not mask real ENOENT.
func TestLockFileENOENTBudgetExhausted(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "workspace.lock")
	prev := openLockRootFn
	openLockRootFn = func(string) (*os.Root, *os.File, string, error) {
		return nil, nil, "", fs.ErrNotExist
	}
	defer func() { openLockRootFn = prev }()

	start := time.Now()
	_, err := LockFile(lockPath, false)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LockFile() error = %v, want ENOENT after budget", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ENOENT retry burned %v, want a bounded budget", elapsed)
	}
}
