package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches git directories for changes and triggers status refreshes
type FileWatcher struct {
	mu sync.Mutex

	watcher    *fsnotify.Watcher
	watching   map[string]bool // worktree root -> watching
	watchPaths map[string][]watchTarget
	onChanged  func(root string)
	closeOnce  sync.Once
	debounce   time.Duration
	lastChange map[string]time.Time
}

type watchTarget struct {
	path  string
	isDir bool
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
		watchPaths: make(map[string][]watchTarget),
		onChanged:  onChanged,
		debounce:   500 * time.Millisecond,
		lastChange: make(map[string]time.Time),
	}

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

	var targets []watchTarget

	if info.IsDir() {
		// Watch .git/index for main repo
		indexPath := filepath.Join(gitPath, "index")
		if target, err := fw.addWatchPath(indexPath); err == nil {
			targets = append(targets, target)
		} else if target, err := fw.addWatchPath(gitPath); err == nil {
			targets = append(targets, target)
		} else {
			return err
		}
	} else {
		// For worktrees, .git is a file pointing to the real gitdir
		gitDir, err := readGitDirFromFile(gitPath)
		if err != nil {
			return err
		}
		indexPath := filepath.Join(gitDir, "index")
		if target, err := fw.addWatchPath(indexPath); err == nil {
			targets = append(targets, target)
		} else if target, err := fw.addWatchPath(gitDir); err == nil {
			targets = append(targets, target)
		} else {
			return err
		}
	}

	fw.watching[root] = true
	fw.watchPaths[root] = targets
	return nil
}

// Unwatch stops watching a worktree
func (fw *FileWatcher) Unwatch(root string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if !fw.watching[root] {
		return
	}

	for _, target := range fw.watchPaths[root] {
		_ = fw.watcher.Remove(target.path)
	}

	delete(fw.watching, root)
	delete(fw.watchPaths, root)
}

// run processes file system events
// Run processes file system events until the context is canceled or the watcher closes.
func (fw *FileWatcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return nil
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
				return nil
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
		if targets, ok := fw.watchPaths[root]; ok {
			for _, target := range targets {
				if path == target.path {
					return root
				}
				if target.isDir && strings.HasPrefix(path, target.path+string(filepath.Separator)) {
					return root
				}
			}
			continue
		}

		// Fallback for legacy entries without watch targets.
		gitPath := filepath.Join(root, ".git")
		if path == gitPath || filepath.Dir(path) == gitPath || strings.HasPrefix(path, gitPath+string(filepath.Separator)) {
			return root
		}
	}
	return ""
}

func (fw *FileWatcher) addWatchPath(path string) (watchTarget, error) {
	info, err := os.Stat(path)
	if err != nil {
		return watchTarget{}, err
	}
	if err := fw.watcher.Add(path); err != nil {
		return watchTarget{}, err
	}
	return watchTarget{path: path, isDir: info.IsDir()}, nil
}

func readGitDirFromFile(gitPath string) (string, error) {
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}

	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("invalid gitdir file: %s", gitPath)
	}

	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if gitDir == "" {
		return "", fmt.Errorf("invalid gitdir file: %s", gitPath)
	}

	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(gitPath), gitDir)
	}
	return filepath.Clean(gitDir), nil
}

// Close stops the watcher and releases resources
func (fw *FileWatcher) Close() error {
	var err error
	fw.closeOnce.Do(func() {
		err = fw.watcher.Close()
	})
	return err
}

// IsWatching checks if a worktree is being watched
func (fw *FileWatcher) IsWatching(root string) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.watching[root]
}
