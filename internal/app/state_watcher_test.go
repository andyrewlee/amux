package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestStateWatcher_IgnoresWorkspaceMetadataWrite(t *testing.T) {
	root := t.TempDir()
	hash := "abc123"
	child := filepath.Join(root, hash)
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: map[string]struct{}{child: {}},
	}

	// Write event on workspace.json inside a child — should be ignored.
	event := fsnotify.Event{
		Name: filepath.Join(child, "workspace.json"),
		Op:   fsnotify.Write,
	}
	if sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return false for workspace.json write")
	}
}

func TestStateWatcher_WatchesNewWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "newdir")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var watched []string
	var mu sync.Mutex
	old := watchMetadataDirFn
	watchMetadataDirFn = func(w *fsnotify.Watcher, dir string) error {
		mu.Lock()
		watched = append(watched, dir)
		mu.Unlock()
		return nil
	}
	defer func() { watchMetadataDirFn = old }()

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
	}

	event := fsnotify.Event{
		Name: newDir,
		Op:   fsnotify.Create,
	}
	if !sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return true for new directory")
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, w := range watched {
		if w == newDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected watchMetadataDir to be called for %s, watched: %v", newDir, watched)
	}
}

func TestStateWatcher_NotifiesOnRegistryWrite(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(registryPath, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notified string
	var mu sync.Mutex
	done := make(chan struct{})

	sw, err := newStateWatcher(registryPath, "", func(reason string) {
		mu.Lock()
		notified = reason
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	sw.debounce = 10 * time.Millisecond
	defer sw.Close()

	go func() { _ = sw.Run(t.Context()) }()

	// Give the watcher a moment to start
	time.Sleep(50 * time.Millisecond)

	// Write to registry
	if err := os.WriteFile(registryPath, []byte(`["new"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for registry notification")
	}

	mu.Lock()
	defer mu.Unlock()
	if notified != "registry" {
		t.Fatalf("expected 'registry' notification, got %q", notified)
	}
}

func TestStateWatcher_IgnoresMetadataRootLockfileEvents(t *testing.T) {
	root := t.TempDir()

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
	}

	// Event on the root itself
	event := fsnotify.Event{
		Name: root,
		Op:   fsnotify.Write,
	}
	if sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return false for root event")
	}
}

func TestStateWatcher_IgnoresChildWatchFailure(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "faildir")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}

	old := watchMetadataDirFn
	watchMetadataDirFn = func(w *fsnotify.Watcher, dir string) error {
		if dir == newDir {
			return fmt.Errorf("injected watch error")
		}
		return nil
	}
	defer func() { watchMetadataDirFn = old }()

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
	}

	// Create event should still return true (workspace dir appeared) even if watch fails
	event := fsnotify.Event{
		Name: newDir,
		Op:   fsnotify.Create,
	}
	if !sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return true even when watch fails")
	}

	// Verify the dir was NOT added to the map (watch failed)
	sw.mu.Lock()
	_, tracked := sw.metadataDirs[newDir]
	sw.mu.Unlock()
	if tracked {
		t.Fatal("expected faildir to not be in metadataDirs after watch failure")
	}
}

func TestStateWatcher_NotifiesOnUnwatchedChildRemoval(t *testing.T) {
	root := t.TempDir()
	unknownDir := filepath.Join(root, "unknownhash")

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
	}

	// Remove event for a dir not in metadataDirs — should still return true
	// because an extensionless basename looks like a directory.
	event := fsnotify.Event{
		Name: unknownDir,
		Op:   fsnotify.Remove,
	}
	if !sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return true for removal of unwatched child")
	}
}
