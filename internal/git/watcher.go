package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
)

// FileWatcher watches git directories for changes and triggers status refreshes
type FileWatcher struct {
	mu sync.Mutex

	watcher     *fsnotify.Watcher
	watching    map[string]bool // workspace root -> watching
	watchPaths  map[string][]watchTarget
	pathToRoot  map[string]string
	onChanged   func(root string)
	closeOnce   sync.Once
	debounce    time.Duration
	lastChange  map[string]time.Time
	watchCount  int
	disabled    bool
	disabledErr error
}

type watchTarget struct {
	path  string
	isDir bool
}

var ErrWatchLimit = errors.New("file watcher limit reached")

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
		pathToRoot: make(map[string]string),
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

	if fw.disabled {
		if fw.disabledErr != nil {
			return fw.disabledErr
		}
		return ErrWatchLimit
	}
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

	fw.watching[root] = true
	fw.watchPaths[root] = targets
	for _, target := range targets {
		targetPath := filepath.Clean(target.path)
		fw.pathToRoot[targetPath] = root
		fw.watchCount++
		perf.Count("git_watcher_watch_add", 1)
	}
	return nil
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
		delete(fw.pathToRoot, filepath.Clean(target.path))
		if fw.watchCount > 0 {
			fw.watchCount--
		}
		perf.Count("git_watcher_watch_remove", 1)
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

			// Debounce: ignore if we just triggered for this root
			fw.mu.Lock()
			if lastChange, ok := fw.lastChange[root]; ok {
				if time.Since(lastChange) < fw.debounce {
					perf.Count("git_watcher_debounce_drop", 1)
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

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return nil
			}
			if err != nil {
				logging.Warn("File watcher error: %v", err)
			}
		}
	}
}

// findRoot finds the workspace root for a given path
func (fw *FileWatcher) findRoot(path string) string {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.pathToRoot != nil {
		cleaned := filepath.Clean(path)
		if root, ok := fw.pathToRoot[cleaned]; ok {
			return root
		}
		dir := cleaned
		for {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
			if root, ok := fw.pathToRoot[dir]; ok {
				return root
			}
		}
	}

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
		if isWatchLimitError(err) {
			if !fw.disabled {
				fw.disabled = true
				fw.disabledErr = fmt.Errorf("%w: %v", ErrWatchLimit, err)
				perf.Count("git_watcher_watch_limit", 1)
				logging.Warn("File watcher limit reached; disabling watcher: %v", err)
			}
			if fw.disabledErr != nil {
				return watchTarget{}, fw.disabledErr
			}
		}
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

func isWatchLimitError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many open files") ||
		strings.Contains(msg, "no space left on device") ||
		strings.Contains(msg, "inotify watch limit")
}
