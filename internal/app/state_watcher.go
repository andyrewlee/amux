package app

import (
	"context"
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

	onChanged func(reason string)
	debounce  time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingReason string
	closed        bool
	closeOnce     sync.Once
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
		if err := watcher.Add(sw.metadataRoot); err != nil {
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
			case sw.isWorkspaceDirEvent(event):
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

func (sw *stateWatcher) isWorkspaceDirEvent(event fsnotify.Event) bool {
	if sw.metadataRoot == "" {
		return false
	}
	name := filepath.Clean(event.Name)
	if name == sw.metadataRoot {
		return false
	}
	if filepath.Dir(name) != sw.metadataRoot {
		return false
	}
	return event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0
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
