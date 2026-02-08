package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Registry manages the projects.json file for persistent project tracking
type Registry struct {
	path string
	mu   sync.RWMutex
}

// registryFile represents the JSON structure of projects.json
// Supports both legacy format (plain array) and new format (object with projects)
type registryFile struct {
	Projects []registryProject `json:"projects"`
}

// registryFileStrings is an alternate format where projects is just string paths
type registryFileStrings struct {
	Projects []string `json:"projects"`
}

type registryProject struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// NewRegistry creates a new registry at the specified path
func NewRegistry(path string) *Registry {
	return &Registry{
		path: path,
	}
}

// Load reads the project paths from the registry file
func (r *Registry) Load() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return nil, err
	}
	defer unlockRegistryFile(lockFile)

	paths, needsRepair, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return nil, err
	}
	if needsRepair {
		if err := r.saveUnlocked(paths); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// Save writes the project paths to the registry file
func (r *Registry) Save(paths []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	return r.saveUnlocked(paths)
}

func (r *Registry) loadUnlockedWithRecovery() ([]string, bool, error) {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		backupPath := r.backupPath()
		backupData, backupErr := os.ReadFile(backupPath)
		if os.IsNotExist(backupErr) {
			return []string{}, false, nil
		}
		if backupErr != nil {
			return nil, false, backupErr
		}
		paths, parseErr := parseRegistryData(backupData, backupPath)
		if parseErr != nil {
			return nil, false, parseErr
		}
		normalized, _ := normalizeAndDedupeProjectPaths(paths)
		return normalized, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	paths, parseErr := parseRegistryData(data, r.path)
	if parseErr == nil {
		normalized, changed := normalizeAndDedupeProjectPaths(paths)
		return normalized, changed, nil
	}

	// If the primary file is corrupted, fall back to a valid backup when available.
	backupPath := r.backupPath()
	backupData, backupErr := os.ReadFile(backupPath)
	if backupErr != nil {
		return nil, false, parseErr
	}
	backupPaths, backupParseErr := parseRegistryData(backupData, backupPath)
	if backupParseErr != nil {
		return nil, false, parseErr
	}
	normalized, _ := normalizeAndDedupeProjectPaths(backupPaths)
	return normalized, true, nil
}

func (r *Registry) saveUnlocked(paths []string) error {
	paths, _ = normalizeAndDedupeProjectPaths(paths)
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Build registry structure
	registry := registryFile{
		Projects: make([]registryProject, len(paths)),
	}
	for i, path := range paths {
		name := filepath.Base(path)
		registry.Projects[i] = registryProject{
			Name: name,
			Path: path,
		}
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	tempPath := r.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := replaceFile(tempPath, r.path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// AddProject adds a project path to the registry
func (r *Registry) AddProject(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path = canonicalProjectPath(path)
	if path == "" {
		return errors.New("project path is required")
	}

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	paths, recoveredFromBackup, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return err
	}

	// Check if already exists
	for _, p := range paths {
		if canonicalProjectPath(p) == path {
			if recoveredFromBackup {
				return r.saveUnlocked(paths)
			}
			return nil // Already registered
		}
	}

	paths = append(paths, path)
	return r.saveUnlocked(paths)
}

// RemoveProject removes a project path from the registry
func (r *Registry) RemoveProject(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path = canonicalProjectPath(path)
	if path == "" {
		return errors.New("project path is required")
	}

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	paths, recoveredFromBackup, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return err
	}

	// Filter out the path
	var newPaths []string
	for _, p := range paths {
		if canonicalProjectPath(p) != path {
			newPaths = append(newPaths, p)
		}
	}
	if len(newPaths) == len(paths) && recoveredFromBackup {
		return r.saveUnlocked(paths)
	}

	return r.saveUnlocked(newPaths)
}

// Projects returns a copy of all registered project paths
func (r *Registry) Projects() ([]string, error) {
	return r.Load()
}

func (r *Registry) lockPath() string {
	return r.path + ".lock"
}

func (r *Registry) backupPath() string {
	return r.path + ".bak"
}

func parseRegistryData(data []byte, path string) ([]string, error) {
	// Try new format first: {"projects": [{name, path}, ...]}
	var registry registryFile
	if err := json.Unmarshal(data, &registry); err == nil {
		paths := make([]string, len(registry.Projects))
		for i, p := range registry.Projects {
			paths[i] = p.Path
		}
		return paths, nil
	}

	// Try alternate format: {"projects": ["path1", "path2"]}
	var registryStrings registryFileStrings
	if err := json.Unmarshal(data, &registryStrings); err == nil {
		return registryStrings.Projects, nil
	}

	// Fall back to legacy format: ["path1", "path2"]
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return paths, nil
}

func canonicalProjectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return filepath.Clean(cleaned)
}

func normalizeAndDedupeProjectPaths(paths []string) ([]string, bool) {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	changed := false
	for _, path := range paths {
		raw := strings.TrimSpace(path)
		if raw == "" {
			if strings.TrimSpace(path) != "" || path != "" {
				changed = true
			}
			continue
		}
		canonical := canonicalProjectPath(raw)
		if canonical == "" {
			changed = true
			continue
		}
		if _, ok := seen[canonical]; ok {
			changed = true
			continue
		}
		seen[canonical] = struct{}{}
		if filepath.Clean(raw) != canonical {
			changed = true
		}
		out = append(out, canonical)
	}
	if len(out) != len(paths) {
		changed = true
	}
	return out, changed
}
