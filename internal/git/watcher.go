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

// excludedDirs contains directories that should not be watched in the working tree.
var excludedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".next":        true,
	"__pycache__":  true,
	"build":        true,
	"dist":         true,
	"target":       true,
	"vendor":       true,
	".venv":        true,
}

// FileWatcher watches git directories for changes and triggers status refreshes
type FileWatcher struct {
	mu sync.Mutex

	watcher    *fsnotify.Watcher
	watching   map[string]bool // workspace root -> watching
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

// Watch starts watching a workspace for git changes
func (fw *FileWatcher) Watch(root string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.watching[root] {
		return nil
	}

	// Watch the .git directory (or .git file for workspaces)
	gitPath := filepath.Join(root, ".git")

	// Check if it's a file (workspace) or directory (main repo)
	info, err := os.Stat(gitPath)
	if err != nil {
		return err
	}

	var targets []watchTarget

	if info.IsDir() {
		// Watch .git directory for main repo (not just the index file)
		// We watch the directory instead of the index file because git does
		// atomic index updates (write temp file, then rename). fsnotify watches
		// inodes, so when git replaces the index, the watch is lost.
		if target, err := fw.addWatchPath(gitPath); err == nil {
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
		// Watch the worktree gitdir directory (not just the index file)
		if target, err := fw.addWatchPath(gitDir); err == nil {
			targets = append(targets, target)
		} else {
			return err
		}
	}

	// Watch working tree directories for file changes
	treeTargets := fw.watchWorkingTree(root)
	targets = append(targets, treeTargets...)

	fw.watching[root] = true
	fw.watchPaths[root] = targets
	return nil
}

// watchWorkingTree recursively watches all directories in the workspace root,
// excluding common build/dependency directories and hidden directories.
func (fw *FileWatcher) watchWorkingTree(root string) []watchTarget {
	var targets []watchTarget
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		// Skip excluded directories
		if excludedDirs[name] {
			return filepath.SkipDir
		}
		// Skip hidden directories (except the root itself)
		if path != root && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if target, err := fw.addWatchPath(path); err == nil {
			targets = append(targets, target)
		}
		return nil
	})
	return targets
}

// Unwatch stops watching a workspace
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

			// Find which workspace this event belongs to
			root := fw.findRoot(event.Name)
			if root == "" {
				continue
			}

			// If a new directory was created, add a watch for it
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					name := filepath.Base(event.Name)
					if !excludedDirs[name] && !strings.HasPrefix(name, ".") {
						fw.mu.Lock()
						// Check root is still being watched (may have been unwatched between findRoot and here)
						if fw.watching[root] {
							if target, err := fw.addWatchPath(event.Name); err == nil {
								fw.watchPaths[root] = append(fw.watchPaths[root], target)
							}
						}
						fw.mu.Unlock()
					}
				}
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

// findRoot finds the workspace root for a given path.
// For nested workspaces, it returns the longest matching root.
func (fw *FileWatcher) findRoot(path string) string {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	sep := string(filepath.Separator)
	var bestRoot string
	bestLen := 0

	for root := range fw.watching {
		// Fast path: working tree events always fall under the workspace root
		if path == root || strings.HasPrefix(path, root+sep) {
			// Prefer longest matching root for nested workspaces
			if len(root) > bestLen {
				bestRoot = root
				bestLen = len(root)
			}
			continue
		}

		// Check explicit watch targets (e.g. worktree gitdir outside root)
		if targets, ok := fw.watchPaths[root]; ok {
			for _, target := range targets {
				if path == target.path {
					if len(root) > bestLen {
						bestRoot = root
						bestLen = len(root)
					}
					break
				}
				if target.isDir && strings.HasPrefix(path, target.path+sep) {
					if len(root) > bestLen {
						bestRoot = root
						bestLen = len(root)
					}
					break
				}
			}
		}
	}
	return bestRoot
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

// IsWatching checks if a workspace is being watched
func (fw *FileWatcher) IsWatching(root string) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.watching[root]
}
