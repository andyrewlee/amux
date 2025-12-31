package git

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches git directories for changes and triggers status refreshes
type FileWatcher struct {
	mu sync.Mutex

	watcher    *fsnotify.Watcher
	watching   map[string]bool // worktree root -> watching
	onChanged  func(root string)
	stopCh     chan struct{}
	debounce   time.Duration
	lastChange map[string]time.Time
}

// NewFileWatcher creates a new file watcher
func NewFileWatcher(onChanged func(root string)) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher:    watcher,
		watching:   make(map[string]bool),
		onChanged:  onChanged,
		stopCh:     make(chan struct{}),
		debounce:   500 * time.Millisecond,
		lastChange: make(map[string]time.Time),
	}

	go fw.run()

	return fw, nil
}

// Watch starts watching a worktree for git changes
func (fw *FileWatcher) Watch(root string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.watching[root] {
		return nil
	}

	// Watch the .git directory (or .git file for worktrees)
	gitPath := filepath.Join(root, ".git")

	// Check if it's a file (worktree) or directory (main repo)
	info, err := os.Stat(gitPath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		// Watch .git/index for main repo
		indexPath := filepath.Join(gitPath, "index")
		if err := fw.watcher.Add(indexPath); err != nil {
			// Try watching the .git directory instead
			if err := fw.watcher.Add(gitPath); err != nil {
				return err
			}
		}
	} else {
		// For worktrees, watch the .git file
		if err := fw.watcher.Add(gitPath); err != nil {
			return err
		}
	}

	fw.watching[root] = true
	return nil
}

// Unwatch stops watching a worktree
func (fw *FileWatcher) Unwatch(root string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if !fw.watching[root] {
		return
	}

	gitPath := filepath.Join(root, ".git")
	fw.watcher.Remove(gitPath)
	fw.watcher.Remove(filepath.Join(gitPath, "index"))

	delete(fw.watching, root)
}

// run processes file system events
func (fw *FileWatcher) run() {
	for {
		select {
		case <-fw.stopCh:
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Find which worktree this event belongs to
			root := fw.findWorktreeRoot(event.Name)
			if root == "" {
				continue
			}

			// Debounce: ignore if we just triggered for this root
			fw.mu.Lock()
			if lastChange, ok := fw.lastChange[root]; ok {
				if time.Since(lastChange) < fw.debounce {
					fw.mu.Unlock()
					continue
				}
			}
			fw.lastChange[root] = time.Now()
			fw.mu.Unlock()

			// Trigger callback
			if fw.onChanged != nil {
				fw.onChanged(root)
			}

		case _, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			// Ignore errors for now
		}
	}
}

// findWorktreeRoot finds the worktree root for a given path
func (fw *FileWatcher) findWorktreeRoot(path string) string {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	for root := range fw.watching {
		gitPath := filepath.Join(root, ".git")
		if path == gitPath || filepath.Dir(path) == gitPath || filepath.HasPrefix(path, gitPath) {
			return root
		}
	}
	return ""
}

// Close stops the watcher and releases resources
func (fw *FileWatcher) Close() error {
	close(fw.stopCh)
	return fw.watcher.Close()
}

// IsWatching checks if a worktree is being watched
func (fw *FileWatcher) IsWatching(root string) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.watching[root]
}
