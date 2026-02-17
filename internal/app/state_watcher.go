package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type stateWatcher struct {
	watcher *fsnotify.Watcher

	registryPath string
	registryDir  string
	metadataRoot string

	onChanged func(reason string, paths []string)
	debounce  time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingReason string
	pendingPaths  map[string]struct{}
	closed        bool
	closeOnce     sync.Once
	metadataDirs  map[string]struct{}

	addWatchFn func(w *fsnotify.Watcher, dir string) error // test hook; nil = use watcher.Add
}

func newStateWatcher(registryPath, metadataRoot string, onChanged func(reason string, paths []string)) (*stateWatcher, error) {
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
				sw.scheduleNotify("registry", filepath.Clean(event.Name))
			case sw.handleMetadataEvent(event):
				sw.scheduleNotify("workspaces", filepath.Clean(event.Name))
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
	if sw.addWatchFn != nil {
		return sw.addWatchFn(sw.watcher, path)
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
		if err := sw.watchMetadataDir(child); err != nil {
			slog.Debug("best-effort watch failed", "path", child, "error", err)
		}
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

	// Workspace metadata writes (e.g. workspace.json) should trigger a refresh so
	// live TUI instances pick up externally created/removed tabs.
	if sw.isWorkspaceMetadataEvent(name, event.Op) {
		return true
	}

	parent := filepath.Dir(name)

	// Direct child of metadata root = workspace directory event.
	if parent == sw.metadataRoot {
		if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
			if sw.looksLikeDir(name) {
				if err := sw.watchMetadataDir(name); err != nil {
					slog.Debug("best-effort watch failed", "path", name, "error", err)
				}
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

func (sw *stateWatcher) isWorkspaceMetadataEvent(path string, op fsnotify.Op) bool {
	if sw.metadataRoot == "" {
		return false
	}
	if op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	if filepath.Base(path) != "workspace.json" {
		return false
	}
	parent := filepath.Dir(path)
	if parent == sw.metadataRoot {
		return false
	}
	return filepath.Dir(parent) == sw.metadataRoot
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

func (sw *stateWatcher) scheduleNotify(reason, path string) {
	if sw.onChanged == nil {
		return
	}
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		return
	}
	if sw.pendingReason != "" && sw.pendingReason != reason {
		sw.pendingPaths = nil
	}
	sw.pendingReason = reason
	if path = strings.TrimSpace(path); path != "" {
		if sw.pendingPaths == nil {
			sw.pendingPaths = make(map[string]struct{})
		}
		sw.pendingPaths[path] = struct{}{}
	}
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
	pathsMap := sw.pendingPaths
	sw.pendingPaths = nil
	sw.timer = nil
	sw.mu.Unlock()

	if sw.onChanged != nil {
		var paths []string
		if len(pathsMap) > 0 {
			paths = make([]string, 0, len(pathsMap))
			for path := range pathsMap {
				paths = append(paths, path)
			}
			sort.Strings(paths)
		}
		sw.onChanged(reason, paths)
	}
}
