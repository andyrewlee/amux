package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireTaskStartLock_AcquiresAndBlocksConcurrent(t *testing.T) {
	home := t.TempDir()
	lock, acquired, err := acquireTaskStartLock(home, "ws-lock", "codex", 2*time.Second)
	if err != nil {
		t.Fatalf("acquireTaskStartLock() error = %v", err)
	}
	if !acquired {
		t.Fatalf("acquireTaskStartLock() acquired = false, want true")
	}
	defer lock.release()

	_, acquiredSecond, err := acquireTaskStartLock(home, "ws-lock", "codex", 2*time.Second)
	if err != nil {
		t.Fatalf("second acquireTaskStartLock() error = %v", err)
	}
	if acquiredSecond {
		t.Fatalf("second acquireTaskStartLock() acquired = true, want false")
	}
}

func TestAcquireTaskStartLock_HeartbeatKeepsActiveLockFresh(t *testing.T) {
	home := t.TempDir()
	ttl := 900 * time.Millisecond

	lock, acquired, err := acquireTaskStartLock(home, "ws-heartbeat", "codex", ttl)
	if err != nil {
		t.Fatalf("acquireTaskStartLock() error = %v", err)
	}
	if !acquired {
		t.Fatalf("acquireTaskStartLock() acquired = false, want true")
	}
	defer lock.release()

	time.Sleep(ttl + 400*time.Millisecond)

	_, acquiredSecond, err := acquireTaskStartLock(home, "ws-heartbeat", "codex", ttl)
	if err != nil {
		t.Fatalf("second acquireTaskStartLock() error = %v", err)
	}
	if acquiredSecond {
		t.Fatalf("second acquireTaskStartLock() acquired = true, want false while first lock is alive")
	}
}

func TestAcquireTaskStartLock_ReclaimsStaleLock(t *testing.T) {
	home := t.TempDir()
	ttl := 200 * time.Millisecond
	lockPath := taskStartLockPath(home, "ws-stale", "codex")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("MkdirAll lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("WriteFile lock: %v", err)
	}
	old := time.Now().Add(-(ttl + 500*time.Millisecond))
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("Chtimes stale lock: %v", err)
	}

	lock, acquired, err := acquireTaskStartLock(home, "ws-stale", "codex", ttl)
	if err != nil {
		t.Fatalf("acquireTaskStartLock() error = %v", err)
	}
	if !acquired {
		t.Fatalf("acquireTaskStartLock() acquired = false, want true after stale lock reclaim")
	}
	lock.release()
	lock.release()
}
