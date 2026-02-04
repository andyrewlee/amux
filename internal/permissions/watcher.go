package permissions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// PermissionWatcher watches workspace .claude/settings.local.json files for permission changes.
type PermissionWatcher struct {
	mu sync.Mutex

	watcher    *fsnotify.Watcher
	watching   map[string]bool            // workspace root -> watching
	watchPaths map[string][]watchTarget   // workspace root -> watched paths
	knownAllow map[string]map[string]bool // workspace root -> set of known allow entries
	onDetected func(root string, newAllow []string)
	closeOnce  sync.Once
	debounce   time.Duration
	lastChange map[string]time.Time
}

type watchTarget struct {
	path  string
	isDir bool
}

// NewPermissionWatcher creates a new permission watcher.
func NewPermissionWatcher(onDetected func(root string, newAllow []string)) (*PermissionWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &PermissionWatcher{
		watcher:    watcher,
		watching:   make(map[string]bool),
		watchPaths: make(map[string][]watchTarget),
		knownAllow: make(map[string]map[string]bool),
		onDetected: onDetected,
		debounce:   2 * time.Second,
		lastChange: make(map[string]time.Time),
	}, nil
}

// Watch starts watching a workspace for permission changes.
func (pw *PermissionWatcher) Watch(root string) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.watching[root] {
		return nil
	}

	claudeDir := filepath.Join(root, ".claude")

	// Read and cache initial state
	pw.cacheAllowState(root)

	var targets []watchTarget

	// Watch .claude directory if it exists
	info, err := os.Stat(claudeDir)
	if err == nil && info.IsDir() {
		if err := pw.watcher.Add(claudeDir); err == nil {
			targets = append(targets, watchTarget{path: claudeDir, isDir: true})
		}
	} else {
		// Watch the workspace root for .claude directory creation
		if err := pw.watcher.Add(root); err == nil {
			targets = append(targets, watchTarget{path: root, isDir: true})
		}
	}

	pw.watching[root] = true
	pw.watchPaths[root] = targets
	return nil
}

// Unwatch stops watching a workspace.
func (pw *PermissionWatcher) Unwatch(root string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if !pw.watching[root] {
		return
	}

	for _, target := range pw.watchPaths[root] {
		_ = pw.watcher.Remove(target.path)
	}

	delete(pw.watching, root)
	delete(pw.watchPaths, root)
	delete(pw.knownAllow, root)
}

// Run processes file system events until the context is canceled.
func (pw *PermissionWatcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-pw.watcher.Events:
			if !ok {
				return nil
			}

			root := pw.findRoot(event.Name)
			if root == "" {
				continue
			}

			// Check if .claude directory was just created (we were watching root)
			claudeDir := filepath.Join(root, ".claude")
			if event.Name == claudeDir && (event.Op&fsnotify.Create != 0) {
				pw.mu.Lock()
				// Switch from watching root to watching .claude
				_ = pw.watcher.Remove(root)
				if err := pw.watcher.Add(claudeDir); err == nil {
					pw.watchPaths[root] = []watchTarget{{path: claudeDir, isDir: true}}
				}
				pw.mu.Unlock()

				// Process any permissions that exist now - we missed the settings.local.json
				// CREATE event because we weren't watching .claude when it was created.
				// Use processChange (not cacheAllowState) so new permissions are reported.
				pw.processChange(root)
				continue
			}

			// Only care about settings.local.json changes
			if filepath.Base(event.Name) != "settings.local.json" {
				continue
			}

			// Debounce
			pw.mu.Lock()
			if lastChange, ok := pw.lastChange[root]; ok {
				if time.Since(lastChange) < pw.debounce {
					pw.mu.Unlock()
					continue
				}
			}
			pw.lastChange[root] = time.Now()
			pw.mu.Unlock()

			// Read current state, diff against known, notify if new
			pw.processChange(root)

		case _, ok := <-pw.watcher.Errors:
			if !ok {
				return nil
			}
		}
	}
}

func (pw *PermissionWatcher) processChange(root string) {
	settingsPath := filepath.Join(root, ".claude", "settings.local.json")
	currentAllow := ReadAllowList(settingsPath)

	pw.mu.Lock()
	known := pw.knownAllow[root]
	if known == nil {
		known = make(map[string]bool)
	}

	var newEntries []string
	for _, perm := range currentAllow {
		trimmed := strings.TrimSpace(perm)
		if trimmed == "" || known[trimmed] {
			continue
		}
		// Skip Edit(**) - this is auto-injected by "allow edits" feature
		if trimmed == "Edit(**)" {
			known[trimmed] = true
			continue
		}
		newEntries = append(newEntries, trimmed)
		known[trimmed] = true
	}
	pw.knownAllow[root] = known
	pw.mu.Unlock()

	if len(newEntries) > 0 && pw.onDetected != nil {
		pw.onDetected(root, newEntries)
	}
}

func (pw *PermissionWatcher) cacheAllowState(root string) {
	settingsPath := filepath.Join(root, ".claude", "settings.local.json")
	currentAllow := ReadAllowList(settingsPath)
	known := make(map[string]bool, len(currentAllow))
	for _, perm := range currentAllow {
		trimmed := strings.TrimSpace(perm)
		if trimmed != "" {
			known[trimmed] = true
		}
	}
	pw.knownAllow[root] = known
}

func (pw *PermissionWatcher) findRoot(path string) string {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	for root := range pw.watching {
		if targets, ok := pw.watchPaths[root]; ok {
			for _, target := range targets {
				if path == target.path {
					return root
				}
				if target.isDir && strings.HasPrefix(path, target.path+string(filepath.Separator)) {
					return root
				}
			}
		}
	}
	return ""
}

// Close stops the watcher and releases resources.
func (pw *PermissionWatcher) Close() error {
	var err error
	pw.closeOnce.Do(func() {
		err = pw.watcher.Close()
	})
	return err
}

// IsWatching checks if a workspace is being watched.
func (pw *PermissionWatcher) IsWatching(root string) bool {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	return pw.watching[root]
}

// ReadAllowList reads the permissions.allow list from a settings.local.json file.
func ReadAllowList(settingsPath string) []string {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}

	var raw struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw.Permissions.Allow
}
