package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileWatcherWatchAndFindRoot(t *testing.T) {
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

	if got := fw.findWorktreeRoot(indexPath); got != root {
		t.Fatalf("findWorktreeRoot() = %s, want %s", got, root)
	}

	fw.Unwatch(root)
	if fw.IsWatching(root) {
		t.Fatalf("expected watcher to be removed")
	}
}
