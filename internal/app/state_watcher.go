package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchMetadataDirFn is a test hook for injecting errors into fsnotify.Add.
var watchMetadataDirFn func(w *fsnotify.Watcher, dir string) error

type stateWatcher struct {
	watcher *fsnotify.Watcher

	registryPath string
	registryDir  string
	metadataRoot string

	onChanged func(reason string)
	debounce  time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingReason string
	closed        bool
	closeOnce     sync.Once
	metadataDirs  map[string]struct{}
}

func newStateWatcher(registryPath, metadataRoot string, onChanged func(reason string)) (*stateWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	sw := &stateWatcher{
		watcher:   watcher,
		onChanged: onChanged,
		debounce:  stateWatcherDebounce,
	}

	if registryPath != "" {
		sw.registryPath = filepath.Clean(registryPath)
		sw.registryDir = filepath.Dir(sw.registryPath)
		if err := watcher.Add(sw.registryDir); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}

	if metadataRoot != "" {
		sw.metadataRoot = filepath.Clean(metadataRoot)
		sw.metadataDirs = make(map[string]struct{})
		if err := sw.watchMetadataRoot(); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}

	return sw, nil
}

func (sw *stateWatcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return nil
			}
			switch {
			case sw.isRegistryEvent(event):
				sw.scheduleNotify("registry")
			case sw.handleMetadataEvent(event):
				sw.scheduleNotify("workspaces")
			}
		case _, ok := <-sw.watcher.Errors:
			if !ok {
				return nil
			}
			// Ignore errors; watcher will continue running.
		}
	}
}

func (sw *stateWatcher) Close() error {
	var err error
	sw.closeOnce.Do(func() {
		sw.mu.Lock()
		sw.closed = true
		if sw.timer != nil {
			sw.timer.Stop()
			sw.timer = nil
		}
		sw.mu.Unlock()
		if sw.watcher != nil {
			err = sw.watcher.Close()
		}
	})
	return err
}

func (sw *stateWatcher) isRegistryEvent(event fsnotify.Event) bool {
	if sw.registryPath == "" {
		return false
	}
	if filepath.Clean(event.Name) != sw.registryPath {
		return false
	}
	return event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0
}

// addWatch delegates to the test hook or the real watcher.
func (sw *stateWatcher) addWatch(path string) error {
	if watchMetadataDirFn != nil {
		return watchMetadataDirFn(sw.watcher, path)
	}
	return sw.watcher.Add(path)
}

// watchMetadataRoot watches the root directory and any existing children.
func (sw *stateWatcher) watchMetadataRoot() error {
	if err := sw.addWatch(sw.metadataRoot); err != nil {
		return err
	}
	entries, err := os.ReadDir(sw.metadataRoot)
	if err != nil {
		// Root may not exist yet; that's fine.
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(sw.metadataRoot, entry.Name())
		_ = sw.watchMetadataDir(child) // best-effort
	}
	return nil
}

// watchMetadataDir registers a child directory for watching.
func (sw *stateWatcher) watchMetadataDir(dir string) error {
	sw.mu.Lock()
	if _, ok := sw.metadataDirs[dir]; ok {
		sw.mu.Unlock()
		return nil
	}
	sw.mu.Unlock()

	// Release lock around slow fsnotify.Add
	if err := sw.addWatch(dir); err != nil {
		return err
	}

	sw.mu.Lock()
	sw.metadataDirs[dir] = struct{}{}
	sw.mu.Unlock()
	return nil
}

// unwatchMetadataDir removes a child directory from watching.
func (sw *stateWatcher) unwatchMetadataDir(dir string) {
	sw.mu.Lock()
	delete(sw.metadataDirs, dir)
	sw.mu.Unlock()
	if sw.watcher != nil {
		_ = sw.watcher.Remove(dir)
	}
}

// handleMetadataEvent classifies a metadata filesystem event.
// Returns true if the event represents a workspace directory change (create/remove).
func (sw *stateWatcher) handleMetadataEvent(event fsnotify.Event) bool {
	if sw.metadataRoot == "" {
		return false
	}
	name := filepath.Clean(event.Name)

	// Ignore events on the root itself (e.g. lockfiles).
	if name == sw.metadataRoot {
		return false
	}

	parent := filepath.Dir(name)

	// Direct child of metadata root = workspace directory event.
	if parent == sw.metadataRoot {
		if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
			if sw.looksLikeDir(name) {
				_ = sw.watchMetadataDir(name) // best-effort
			}
			return true
		}
		if event.Op&fsnotify.Remove != 0 {
			sw.unwatchMetadataDir(name)
			return true
		}
		return false
	}

	// Deeper nesting (e.g. workspace.json writes) — ignore.
	return false
}

// looksLikeDir returns true if the path is likely a directory.
// Falls back to checking for an extensionless basename when stat fails
// (e.g. the path was already removed).
func (sw *stateWatcher) looksLikeDir(path string) bool {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir()
	}
	return filepath.Ext(filepath.Base(path)) == ""
}

func (sw *stateWatcher) scheduleNotify(reason string) {
	if sw.onChanged == nil {
		return
	}
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		return
	}
	sw.pendingReason = reason
	if sw.timer == nil {
		sw.timer = time.AfterFunc(sw.debounce, sw.fire)
	} else {
		sw.timer.Reset(sw.debounce)
	}
	sw.mu.Unlock()
}

func (sw *stateWatcher) fire() {
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		return
	}
	reason := sw.pendingReason
	sw.pendingReason = ""
	sw.timer = nil
	sw.mu.Unlock()

	if sw.onChanged != nil {
		sw.onChanged(reason)
	}
}
