package git

import (
	"os"
	"path/filepath"
	"testing"
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
}
