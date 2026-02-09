package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateWatcher_IgnoresWorkspaceMetadataWrite(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	workspaceDir := filepath.Join(metadataRoot, "ws-1")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	workspaceFile := filepath.Join(workspaceDir, "workspace.json")
	if err := os.WriteFile(workspaceFile, []byte(`{"name":"a"}`), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)
	time.Sleep(60 * time.Millisecond)
	drainStateWatcherReasons(reasons)

	if err := os.WriteFile(workspaceFile, []byte(`{"name":"b"}`), 0o644); err != nil {
		t.Fatalf("update workspace file: %v", err)
	}
	ensureNoStateWatcherReason(t, reasons, 250*time.Millisecond)
	_ = sw
}

func TestStateWatcher_WatchesNewWorkspaceDirectory(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)

	workspaceDir := filepath.Join(metadataRoot, "ws-2")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	waitForStateWatcherReason(t, reasons, "workspaces", 2*time.Second)
	time.Sleep(60 * time.Millisecond)
	drainStateWatcherReasons(reasons)

	workspaceFile := filepath.Join(workspaceDir, "workspace.json")
	if err := os.WriteFile(workspaceFile, []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	ensureNoStateWatcherReason(t, reasons, 250*time.Millisecond)

	if err := os.WriteFile(workspaceFile, []byte(`{"name":"y"}`), 0o644); err != nil {
		t.Fatalf("rewrite workspace file: %v", err)
	}
	ensureNoStateWatcherReason(t, reasons, 250*time.Millisecond)
	_ = sw
}

func TestStateWatcher_NotifiesOnRegistryWrite(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)

	if err := os.WriteFile(registryPath, []byte(`{"projects":["/tmp/repo"]}`), 0o644); err != nil {
		t.Fatalf("update registry: %v", err)
	}
	waitForStateWatcherReason(t, reasons, "registry", 2*time.Second)
	_ = sw
}

func TestStateWatcher_IgnoresMetadataRootLockfileEvents(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)
	time.Sleep(60 * time.Millisecond)
	drainStateWatcherReasons(reasons)

	lockPath := filepath.Join(metadataRoot, "ws-1.lock")
	if err := os.WriteFile(lockPath, []byte("lock"), 0o644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	ensureNoStateWatcherReason(t, reasons, 250*time.Millisecond)

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove lock file: %v", err)
	}
	ensureNoStateWatcherReason(t, reasons, 250*time.Millisecond)
	_ = sw
}

func TestStateWatcher_IgnoresChildWatchFailure(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	goodDir := filepath.Join(metadataRoot, "ws-good")
	if err := os.MkdirAll(goodDir, 0o755); err != nil {
		t.Fatalf("mkdir good dir: %v", err)
	}
	badDir := filepath.Join(metadataRoot, "ws-bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatalf("mkdir bad dir: %v", err)
	}
	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	origWatchMetadataDirFn := watchMetadataDirFn
	watchMetadataDirFn = func(sw *stateWatcher, dir string) error {
		if filepath.Clean(dir) == filepath.Clean(badDir) {
			return errors.New("watch failed")
		}
		return origWatchMetadataDirFn(sw, dir)
	}
	t.Cleanup(func() {
		watchMetadataDirFn = origWatchMetadataDirFn
	})

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)
	sw.mu.Lock()
	_, hasGoodWatch := sw.metadataDirs[filepath.Clean(goodDir)]
	_, hasBadWatch := sw.metadataDirs[filepath.Clean(badDir)]
	sw.mu.Unlock()
	if !hasGoodWatch {
		t.Fatalf("expected watcher to include %s", goodDir)
	}
	if hasBadWatch {
		t.Fatalf("expected watcher to skip %s after watch failure", badDir)
	}

	// Ensure watcher still runs by receiving registry updates.
	if err := os.WriteFile(registryPath, []byte(`{"projects":["/tmp/repo"]}`), 0o644); err != nil {
		t.Fatalf("update registry: %v", err)
	}
	waitForStateWatcherReason(t, reasons, "registry", 2*time.Second)

	// Ensure metadata-root workspace events still flow.
	newDir := filepath.Join(metadataRoot, "ws-new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	waitForStateWatcherReason(t, reasons, "workspaces", 2*time.Second)
	_ = sw
}

func TestStateWatcher_NotifiesOnUnwatchedChildRemoval(t *testing.T) {
	metadataRoot := filepath.Join(t.TempDir(), "workspaces-metadata")
	if err := os.MkdirAll(metadataRoot, 0o755); err != nil {
		t.Fatalf("mkdir metadata root: %v", err)
	}
	childDir := filepath.Join(metadataRoot, "abcdef1234567890")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("mkdir child dir: %v", err)
	}

	registryPath := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(registryPath, []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	// Make watchMetadataDirFn fail for the child so it is never registered.
	origWatchMetadataDirFn := watchMetadataDirFn
	watchMetadataDirFn = func(sw *stateWatcher, dir string) error {
		if filepath.Clean(dir) == filepath.Clean(childDir) {
			return errors.New("simulated watch failure")
		}
		return origWatchMetadataDirFn(sw, dir)
	}
	t.Cleanup(func() {
		watchMetadataDirFn = origWatchMetadataDirFn
	})

	reasons := make(chan string, 16)
	sw := startStateWatcherForTest(t, registryPath, metadataRoot, reasons)

	// Confirm the child is NOT in metadataDirs.
	sw.mu.Lock()
	_, hasChild := sw.metadataDirs[filepath.Clean(childDir)]
	sw.mu.Unlock()
	if hasChild {
		t.Fatalf("expected child dir to NOT be watched")
	}

	time.Sleep(60 * time.Millisecond)
	drainStateWatcherReasons(reasons)

	// Remove the unwatched child directory.
	if err := os.RemoveAll(childDir); err != nil {
		t.Fatalf("remove child dir: %v", err)
	}

	// The removal should still trigger a "workspaces" notification.
	waitForStateWatcherReason(t, reasons, "workspaces", 2*time.Second)
}

func startStateWatcherForTest(t *testing.T, registryPath, metadataRoot string, reasons chan<- string) *stateWatcher {
	t.Helper()

	sw, err := newStateWatcher(registryPath, metadataRoot, func(reason string) {
		select {
		case reasons <- reason:
		default:
		}
	})
	if err != nil {
		t.Fatalf("newStateWatcher: %v", err)
	}
	sw.debounce = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sw.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		_ = sw.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("state watcher did not stop in time")
		}
	})

	return sw
}

func waitForStateWatcherReason(t *testing.T, reasons <-chan string, want string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case got := <-reasons:
			if got == want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for reason %q", want)
		}
	}
}

func ensureNoStateWatcherReason(t *testing.T, reasons <-chan string, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case got := <-reasons:
		t.Fatalf("unexpected state watcher reason %q", got)
	case <-timer.C:
	}
}

func drainStateWatcherReasons(reasons <-chan string) {
	for {
		select {
		case <-reasons:
		default:
			return
		}
	}
}
