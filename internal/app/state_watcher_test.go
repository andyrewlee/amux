package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestStateWatcher_NotifiesOnWorkspaceMetadataWrite(t *testing.T) {
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

	// Write event on workspace.json inside a child should trigger refresh.
	event := fsnotify.Event{
		Name: filepath.Join(child, "workspace.json"),
		Op:   fsnotify.Write,
	}
	if !sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to return true for workspace.json write")
	}
}

func TestStateWatcher_IgnoresNonWorkspaceMetadataNestedWrite(t *testing.T) {
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

	event := fsnotify.Event{
		Name: filepath.Join(child, "scratch.tmp"),
		Op:   fsnotify.Write,
	}
	if sw.handleMetadataEvent(event) {
		t.Fatal("expected handleMetadataEvent to ignore non-workspace metadata file writes")
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

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
		addWatchFn: func(w *fsnotify.Watcher, dir string) error {
			mu.Lock()
			watched = append(watched, dir)
			mu.Unlock()
			return nil
		},
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

func TestStateWatcher_WatchMetadataDirSkipsClosedWatcher(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "newdir")

	called := false
	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
		closed:       true,
		addWatchFn: func(_ *fsnotify.Watcher, _ string) error {
			called = true
			return nil
		},
	}

	if err := sw.watchMetadataDir(newDir); err != nil {
		t.Fatalf("watchMetadataDir: %v", err)
	}
	if called {
		t.Fatal("expected closed watcher to skip addWatch")
	}
	sw.mu.Lock()
	_, tracked := sw.metadataDirs[newDir]
	sw.mu.Unlock()
	if tracked {
		t.Fatal("expected closed watcher to not track new metadata dir")
	}
}

func TestStateWatcher_WatchMetadataDirDoesNotTrackAfterClose(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "newdir")

	addStarted := make(chan struct{})
	releaseAdd := make(chan struct{})
	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
		addWatchFn: func(_ *fsnotify.Watcher, _ string) error {
			close(addStarted)
			<-releaseAdd
			return nil
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- sw.watchMetadataDir(newDir)
	}()

	select {
	case <-addStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for addWatch")
	}

	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(releaseAdd)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watchMetadataDir: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watchMetadataDir")
	}

	sw.mu.Lock()
	_, tracked := sw.metadataDirs[newDir]
	sw.mu.Unlock()
	if tracked {
		t.Fatal("expected metadata dir to not be tracked after Close")
	}
}

func TestStateWatcher_NotifiesOnRegistryWrite(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(registryPath, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reasons are delivered on a channel so we can wait specifically for the
	// "registry" notification, ignoring the one-shot reconcile that Run emits at
	// startup (registration is now deferred into Run, so it reconciles once).
	reasons := make(chan string, 4)

	sw, err := newStateWatcher(registryPath, "", func(reason string, paths []string) {
		select {
		case reasons <- reason:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	sw.debounce = 10 * time.Millisecond
	defer sw.Close()

	go func() { _ = sw.Run(t.Context()) }()

	// Give the watcher a moment to register and emit its startup reconcile.
	time.Sleep(50 * time.Millisecond)

	// Write to registry
	if err := os.WriteFile(registryPath, []byte(`["new"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case reason := <-reasons:
			if reason == "registry" {
				return
			}
			// Skip the startup reconcile (delivered as "workspaces").
		case <-deadline:
			t.Fatal("timed out waiting for registry notification")
		}
	}
}

func TestStateWatcher_ConstructorIsLazy(t *testing.T) {
	// The constructor must be O(1): it should NOT read the metadata root or add
	// any watches. Registration is deferred to Run.
	root := t.TempDir()
	for _, name := range []string{"ws1", "ws2"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registryPath := filepath.Join(t.TempDir(), "projects.json")

	sw, err := newStateWatcher(registryPath, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sw.Close()

	sw.mu.Lock()
	n := len(sw.metadataDirs)
	sw.mu.Unlock()
	if n != 0 {
		t.Fatalf("constructor registered %d metadata dirs; expected 0 (registration is deferred to Run)", n)
	}
}

func TestStateWatcher_RegisterWatchesRootAndExistingDirs(t *testing.T) {
	// After register() runs (called at the start of Run), the metadata root,
	// each existing workspace dir, and the registry dir must be watched.
	root := t.TempDir()
	existing := filepath.Join(root, "abc123")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	registryDir := t.TempDir()
	registryPath := filepath.Join(registryDir, "projects.json")

	var mu sync.Mutex
	watched := make(map[string]struct{})

	sw, err := newStateWatcher(registryPath, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sw.Close()
	sw.addWatchFn = func(_ *fsnotify.Watcher, dir string) error {
		mu.Lock()
		watched[dir] = struct{}{}
		mu.Unlock()
		return nil
	}

	if err := sw.register(); err != nil {
		t.Fatalf("register: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{root, existing, registryDir} {
		if _, ok := watched[want]; !ok {
			t.Fatalf("expected %s to be watched after register; watched: %v", want, watched)
		}
	}

	sw.mu.Lock()
	_, tracked := sw.metadataDirs[existing]
	sw.mu.Unlock()
	if !tracked {
		t.Fatalf("expected existing workspace dir %s to be tracked", existing)
	}
}

func TestStateWatcher_RegisterReportsMetadataRootFailure(t *testing.T) {
	// A metadata root whose Add fails must NOT make construction fatal; it should
	// fail at deferred registration time so Run can surface disabled sync through
	// the supervisor instead of staying alive on an unregistered watcher.
	root := t.TempDir()
	sw, err := newStateWatcher("", root, nil)
	if err != nil {
		t.Fatalf("constructor must not fail on metadata dir issues: %v", err)
	}
	defer sw.Close()
	sw.addWatchFn = func(_ *fsnotify.Watcher, _ string) error {
		return errors.New("injected add failure")
	}

	if err := sw.register(); err == nil {
		t.Fatal("expected register to report metadata root watch failure")
	}
}

func TestStateWatcher_RegisterReportsRegistryDirFailure(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "projects.json")
	sw, err := newStateWatcher(registryPath, "", nil)
	if err != nil {
		t.Fatalf("constructor must not fail on registry dir issues: %v", err)
	}
	defer sw.Close()
	sw.addWatchFn = func(_ *fsnotify.Watcher, _ string) error {
		return errors.New("injected add failure")
	}

	if err := sw.register(); err == nil {
		t.Fatal("expected register to report registry dir watch failure")
	}
}

func TestStateWatcher_RunEmitsStartupReconcile(t *testing.T) {
	// Registration is deferred into Run, so a change in the construct->register
	// window would be missed; Run must emit exactly one reconcile so nothing is
	// permanently lost.
	root := t.TempDir()

	reconciled := make(chan string, 4)
	sw, err := newStateWatcher("", root, func(reason string, _ []string) {
		select {
		case reconciled <- reason:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	sw.debounce = 5 * time.Millisecond
	defer sw.Close()

	go func() { _ = sw.Run(t.Context()) }()

	select {
	case reason := <-reconciled:
		if reason != "workspaces" {
			t.Fatalf("startup reconcile reason = %q, want %q", reason, "workspaces")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for startup reconcile")
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

	sw := &stateWatcher{
		metadataRoot: root,
		metadataDirs: make(map[string]struct{}),
		addWatchFn: func(w *fsnotify.Watcher, dir string) error {
			if dir == newDir {
				return errors.New("injected watch error")
			}
			return nil
		},
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

func TestStateWatcher_ReasonChangeResetsPendingPaths(t *testing.T) {
	var mu sync.Mutex
	var gotReason string
	var gotPaths []string

	sw := &stateWatcher{
		debounce: 50 * time.Millisecond,
		onChanged: func(reason string, paths []string) {
			mu.Lock()
			gotReason = reason
			gotPaths = paths
			mu.Unlock()
		},
	}

	// Schedule a "registry" event with a path.
	sw.scheduleNotify("registry", "/path/to/registry.json")

	// Before the timer fires, schedule a "workspaces" event with a different path.
	sw.scheduleNotify("workspaces", "/path/to/workspace.json")

	// Wait for the debounce to fire.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if gotReason != "workspaces" {
		t.Fatalf("reason = %q, want %q", gotReason, "workspaces")
	}
	// The registry path should have been discarded when the reason changed.
	for _, p := range gotPaths {
		if p == "/path/to/registry.json" {
			t.Fatal("expected registry path to be discarded when reason changed to workspaces")
		}
	}
	if len(gotPaths) != 1 || gotPaths[0] != "/path/to/workspace.json" {
		t.Fatalf("paths = %v, want [/path/to/workspace.json]", gotPaths)
	}
}
