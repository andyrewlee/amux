package git

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileWatcher(t *testing.T) {
	t.Run("watch and unwatch", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0o644); err != nil {
			t.Fatalf("write index: %v", err)
		}

		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}
		if !fw.IsWatching(root) {
			t.Fatalf("expected watcher to be active")
		}

		fw.Unwatch(root)
		if fw.IsWatching(root) {
			t.Fatalf("expected watcher to be removed")
		}
	})

	t.Run("findRoot", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0o644); err != nil {
			t.Fatalf("write index: %v", err)
		}

		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		if got := fw.findRoot(indexPath); got != root {
			t.Fatalf("findRoot() = %s, want %s", got, root)
		}
	})

	t.Run("workspace gitdir file", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(t.TempDir(), "gitdir")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir gitdir: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0o644); err != nil {
			t.Fatalf("write index: %v", err)
		}

		gitFile := filepath.Join(root, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: "+gitDir), 0o644); err != nil {
			t.Fatalf("write .git file: %v", err)
		}

		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		if got := fw.findRoot(indexPath); got != root {
			t.Fatalf("findRoot() = %s, want %s", got, root)
		}
	})

	t.Run("watch non-existent path", func(t *testing.T) {
		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		err = fw.Watch("/nonexistent/path/to/repo")
		if err == nil {
			t.Fatalf("Watch() should fail for non-existent path")
		}
	})

	t.Run("unwatch non-watched path", func(t *testing.T) {
		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		// Should not panic or error
		fw.Unwatch("/nonexistent/path")
		if fw.IsWatching("/nonexistent/path") {
			t.Fatalf("expected IsWatching to return false for non-watched path")
		}
	})

	t.Run("double watch same path", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}

		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		// Second watch should not error (idempotent)
		if err := fw.Watch(root); err != nil {
			t.Fatalf("second Watch() error = %v", err)
		}

		if !fw.IsWatching(root) {
			t.Fatalf("expected watcher to still be active")
		}
	})

	t.Run("detects changes after atomic file replacement", func(t *testing.T) {
		// This test simulates what git does when committing:
		// 1. Write to a temp file
		// 2. Rename temp file over the index file (atomic replacement)
		// The watcher should still detect changes after this replacement.
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte("initial"), 0o644); err != nil {
			t.Fatalf("write index: %v", err)
		}

		var notifyCount atomic.Int32
		fw, err := NewFileWatcher(func(r string) {
			if r == root {
				notifyCount.Add(1)
			}
		})
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		// Reduce debounce for faster test
		fw.debounce = 50 * time.Millisecond

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			_ = fw.Run(ctx)
		}()

		// Give the watcher time to start
		time.Sleep(50 * time.Millisecond)

		// Simulate atomic file replacement (like git commit does)
		tempPath := filepath.Join(gitDir, "index.lock")
		if err := os.WriteFile(tempPath, []byte("after commit"), 0o644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		if err := os.Rename(tempPath, indexPath); err != nil {
			t.Fatalf("rename: %v", err)
		}

		// Wait for the notification by polling rather than a fixed sleep.
		waitForNotifyCount(t, &notifyCount, 1, 2*time.Second)

		// Reset counter and wait out the debounce window before the next change.
		// This is a debounce wait, not a notification wait — the count is
		// intentionally 0 here.
		notifyCount.Store(0)
		time.Sleep(2 * fw.debounce)

		// Make another change - this verifies the watch is still active
		tempPath2 := filepath.Join(gitDir, "index.lock")
		if err := os.WriteFile(tempPath2, []byte("second commit"), 0o644); err != nil {
			t.Fatalf("write temp2: %v", err)
		}
		if err := os.Rename(tempPath2, indexPath); err != nil {
			t.Fatalf("rename2: %v", err)
		}

		// Wait for the second notification.
		waitForNotifyCount(t, &notifyCount, 1, 2*time.Second)
	})
}

// waitForNotifyCount polls c until it reaches at least want or the timeout
// elapses, failing the test on timeout. Used in place of a fixed sleep plus an
// immediate Load, which is flaky on slow CI and wastes time on fast machines.
func waitForNotifyCount(t *testing.T, c *atomic.Int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.Load() >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for notify count >= %d (got %d)", timeout, want, c.Load())
}
