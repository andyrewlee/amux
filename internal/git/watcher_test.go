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
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0644); err != nil {
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
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0644); err != nil {
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

	t.Run("findRoot prefers longest match for nested workspaces", func(t *testing.T) {
		// Create outer workspace
		outer := t.TempDir()
		outerGit := filepath.Join(outer, ".git")
		if err := os.MkdirAll(outerGit, 0755); err != nil {
			t.Fatalf("mkdir outer .git: %v", err)
		}

		// Create inner workspace nested inside outer
		inner := filepath.Join(outer, "projects", "inner")
		innerGit := filepath.Join(inner, ".git")
		if err := os.MkdirAll(innerGit, 0755); err != nil {
			t.Fatalf("mkdir inner .git: %v", err)
		}

		fw, err := NewFileWatcher(nil)
		if err != nil {
			t.Fatalf("NewFileWatcher() error = %v", err)
		}
		defer func() {
			_ = fw.Close()
		}()

		// Watch both workspaces
		if err := fw.Watch(outer); err != nil {
			t.Fatalf("Watch(outer) error = %v", err)
		}
		if err := fw.Watch(inner); err != nil {
			t.Fatalf("Watch(inner) error = %v", err)
		}

		// Event in inner workspace should match inner, not outer
		innerFile := filepath.Join(inner, "src", "main.go")
		if got := fw.findRoot(innerFile); got != inner {
			t.Fatalf("findRoot(%s) = %s, want %s (longest match)", innerFile, got, inner)
		}

		// Event in outer workspace should match outer
		outerFile := filepath.Join(outer, "README.md")
		if got := fw.findRoot(outerFile); got != outer {
			t.Fatalf("findRoot(%s) = %s, want %s", outerFile, got, outer)
		}
	})

	t.Run("workspace gitdir file", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(t.TempDir(), "gitdir")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir gitdir: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte(""), 0644); err != nil {
			t.Fatalf("write index: %v", err)
		}

		gitFile := filepath.Join(root, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: "+gitDir), 0644); err != nil {
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
		if err := os.MkdirAll(gitDir, 0755); err != nil {
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

	t.Run("detects working tree file creation", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}

		// Create a subdirectory in the working tree
		srcDir := filepath.Join(root, "src")
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			t.Fatalf("mkdir src: %v", err)
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

		fw.debounce = 50 * time.Millisecond

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			_ = fw.Run(ctx)
		}()

		time.Sleep(50 * time.Millisecond)

		// Create a new file in the working tree via atomic save (temp + rename)
		tempPath := filepath.Join(srcDir, ".main.go.tmp")
		if err := os.WriteFile(tempPath, []byte("package main"), 0644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		finalPath := filepath.Join(srcDir, "main.go")
		if err := os.Rename(tempPath, finalPath); err != nil {
			t.Fatalf("rename: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		if notifyCount.Load() == 0 {
			t.Fatalf("expected notification after working tree file creation")
		}
	})

	t.Run("watches dynamically created directories", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
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

		fw.debounce = 50 * time.Millisecond

		if err := fw.Watch(root); err != nil {
			t.Fatalf("Watch() error = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			_ = fw.Run(ctx)
		}()

		time.Sleep(50 * time.Millisecond)

		// Create a new directory after watching started
		newDir := filepath.Join(root, "newpkg")
		if err := os.MkdirAll(newDir, 0755); err != nil {
			t.Fatalf("mkdir newpkg: %v", err)
		}

		// Wait for the directory create event to be processed and watch added
		time.Sleep(100 * time.Millisecond)
		notifyCount.Store(0)

		// Create a file in the new directory via atomic save
		tempPath := filepath.Join(newDir, ".file.tmp")
		if err := os.WriteFile(tempPath, []byte("data"), 0644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		if err := os.Rename(tempPath, filepath.Join(newDir, "file.go")); err != nil {
			t.Fatalf("rename: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		if notifyCount.Load() == 0 {
			t.Fatalf("expected notification after file creation in dynamically watched directory")
		}
	})

	t.Run("excludes node_modules from watching", func(t *testing.T) {
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}

		// Create node_modules directory
		nmDir := filepath.Join(root, "node_modules")
		if err := os.MkdirAll(filepath.Join(nmDir, "some-package"), 0755); err != nil {
			t.Fatalf("mkdir node_modules: %v", err)
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

		// Verify node_modules paths are not in watch targets
		fw.mu.Lock()
		targets := fw.watchPaths[root]
		fw.mu.Unlock()

		for _, target := range targets {
			if filepath.Base(target.path) == "node_modules" {
				t.Fatalf("node_modules should not be watched, but found: %s", target.path)
			}
		}
	})

	t.Run("detects changes after atomic file replacement", func(t *testing.T) {
		// This test simulates what git does when committing:
		// 1. Write to a temp file
		// 2. Rename temp file over the index file (atomic replacement)
		// The watcher should still detect changes after this replacement.
		root := t.TempDir()
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatalf("mkdir .git: %v", err)
		}
		indexPath := filepath.Join(gitDir, "index")
		if err := os.WriteFile(indexPath, []byte("initial"), 0644); err != nil {
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
		if err := os.WriteFile(tempPath, []byte("after commit"), 0644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		if err := os.Rename(tempPath, indexPath); err != nil {
			t.Fatalf("rename: %v", err)
		}

		// Wait for notification
		time.Sleep(100 * time.Millisecond)

		if notifyCount.Load() == 0 {
			t.Fatalf("expected notification after atomic file replacement")
		}

		// Reset counter and wait for debounce
		notifyCount.Store(0)
		time.Sleep(100 * time.Millisecond)

		// Make another change - this verifies the watch is still active
		tempPath2 := filepath.Join(gitDir, "index.lock")
		if err := os.WriteFile(tempPath2, []byte("second commit"), 0644); err != nil {
			t.Fatalf("write temp2: %v", err)
		}
		if err := os.Rename(tempPath2, indexPath); err != nil {
			t.Fatalf("rename2: %v", err)
		}

		// Wait for notification
		time.Sleep(100 * time.Millisecond)

		if notifyCount.Load() == 0 {
			t.Fatalf("expected notification after second atomic file replacement")
		}
	})
}
