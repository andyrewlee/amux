package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type stateWatcher struct {
	watcher *fsnotify.Watcher

	registryPath string
	registryDir  string
	metadataRoot string
	metadataDirs map[string]struct{}

	onChanged func(reason string)
	debounce  time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingReason string
	closed        bool
	closeOnce     sync.Once
}

var watchMetadataDirFn = func(sw *stateWatcher, dir string) error {
	return sw.watchMetadataDir(dir)
}

func newStateWatcher(registryPath, metadataRoot string, onChanged func(reason string)) (*stateWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	sw := &stateWatcher{
		watcher:      watcher,
		onChanged:    onChanged,
		debounce:     stateWatcherDebounce,
		metadataDirs: make(map[string]struct{}),
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

func (sw *stateWatcher) watchMetadataRoot() error {
	if sw.metadataRoot == "" {
		return nil
	}
	if err := watchMetadataDirFn(sw, sw.metadataRoot); err != nil {
		return err
	}
	entries, err := os.ReadDir(sw.metadataRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(sw.metadataRoot, entry.Name())
		if err := watchMetadataDirFn(sw, dir); err != nil && !os.IsNotExist(err) {
			// Degrade gracefully when one metadata child cannot be watched.
			continue
		}
	}
	return nil
}

func (sw *stateWatcher) watchMetadataDir(dir string) error {
	if sw.watcher == nil {
		return nil
	}
	clean := filepath.Clean(dir)
	if clean == "" {
		return nil
	}
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		return nil
	}
	if _, ok := sw.metadataDirs[clean]; ok {
		sw.mu.Unlock()
		return nil
	}
	sw.mu.Unlock()
	if err := sw.watcher.Add(clean); err != nil {
		return err
	}
	sw.mu.Lock()
	if sw.closed {
		sw.mu.Unlock()
		_ = sw.watcher.Remove(clean)
		return nil
	}
	sw.metadataDirs[clean] = struct{}{}
	sw.mu.Unlock()
	return nil
}

func (sw *stateWatcher) unwatchMetadataDir(dir string) {
	if sw.watcher == nil {
		return
	}
	clean := filepath.Clean(dir)
	if clean == "" {
		return
	}
	sw.mu.Lock()
	if _, ok := sw.metadataDirs[clean]; !ok {
		sw.mu.Unlock()
		return
	}
	delete(sw.metadataDirs, clean)
	sw.mu.Unlock()
	_ = sw.watcher.Remove(clean)
}

func (sw *stateWatcher) isWatchedMetadataDir(dir string) bool {
	clean := filepath.Clean(dir)
	if clean == "" {
		return false
	}
	sw.mu.Lock()
	_, ok := sw.metadataDirs[clean]
	sw.mu.Unlock()
	return ok
}

func (sw *stateWatcher) handleMetadataEvent(event fsnotify.Event) bool {
	if sw.metadataRoot == "" {
		return false
	}
	name := filepath.Clean(event.Name)
	if name == sw.metadataRoot {
		return false
	}
	op := event.Op & (fsnotify.Write | fsnotify.Create | fsnotify.Remove | fsnotify.Rename)
	if op == 0 {
		return false
	}

	// Immediate children are workspace metadata directories. Watch/unwatch as
	// they appear/disappear to catch nested workspace.json writes.
	if filepath.Dir(name) == sw.metadataRoot {
		isWorkspaceDir := false
		if info, err := os.Stat(name); err == nil {
			isWorkspaceDir = info.IsDir()
		} else if os.IsNotExist(err) && op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			// Removal/rename can race with stat; trust watcher state when present.
			isWorkspaceDir = sw.isWatchedMetadataDir(name)
		}
		if !isWorkspaceDir {
			return false
		}
		if op&(fsnotify.Create|fsnotify.Rename) != 0 {
			_ = sw.watchMetadataDir(name)
		}
		if op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			sw.unwatchMetadataDir(name)
		}
		return true
	}

	// Ignore nested metadata file writes (e.g. workspace.json saves). These are
	// frequent during normal local tab persistence and should not trigger a full
	// project reload cycle.
	return false
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
