//go:build !windows

package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockRegistryFileRetriesWhenWaiterAcquiresUnlinkedInode(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "workspace.lock")
	held, err := lockRegistryFile(lockPath, false)
	if err != nil {
		t.Fatalf("initial lockRegistryFile() error = %v", err)
	}

	waiter := make(chan *os.File, 1)
	waitErr := make(chan error, 1)
	go func() {
		file, lockErr := lockRegistryFile(lockPath, false)
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
	replacement, err := lockRegistryFile(lockPath, false)
	if err != nil {
		t.Fatalf("replacement lockRegistryFile() error = %v", err)
	}
	unlockRegistryFile(held)

	select {
	case err := <-waitErr:
		unlockRegistryFile(replacement)
		t.Fatalf("waiter lockRegistryFile() error = %v", err)
	case file := <-waiter:
		unlockRegistryFile(file)
		unlockRegistryFile(replacement)
		t.Fatal("waiter acquired the unlinked inode while the replacement lock was held")
	case <-time.After(100 * time.Millisecond):
	}

	unlockRegistryFile(replacement)
	select {
	case err := <-waitErr:
		t.Fatalf("waiter lockRegistryFile() error = %v", err)
	case file := <-waiter:
		unlockRegistryFile(file)
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not acquire the replacement lock after it was released")
	}
}
