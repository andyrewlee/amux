package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/andyrewlee/amux/internal/fsatomic"
	"github.com/andyrewlee/amux/internal/validation"
)

// Registry manages the projects.json file for persistent project tracking
type Registry struct {
	path string
	mu   sync.RWMutex
}

// RegistryEntry is one registered project as stored in projects.json: the
// project's name — the filesystem segment under the workspaces root its
// workspaces live in — and the repository path the name was minted from.
// The JSON tags make it the wire type directly: one shape, no conversion.
type RegistryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// registryFileVersion is the newest projects.json schema written and
// readable. v0 = the pre-versioning shape (no "version" key).
const registryFileVersion = 1

// registryFile represents the JSON structure of projects.json
type registryFile struct {
	Version  int             `json:"version"`
	Projects []RegistryEntry `json:"projects"`
}

// NewRegistry creates a new registry at the specified path
func NewRegistry(path string) *Registry {
	return &Registry{
		path: path,
	}
}

// Load reads the project paths from the registry file
func (r *Registry) Load() ([]string, error) {
	entries, err := r.Entries()
	if err != nil {
		return nil, err
	}
	return EntryPaths(entries), nil
}

// EntryPaths projects registry entries to their path list — the one
// implementation of the entries→paths view (Load and the service's load
// flow share it, so the shape stays in step as the entry evolves).
func EntryPaths(entries []RegistryEntry) []string {
	paths := make([]string, len(entries))
	for i := range entries {
		paths[i] = entries[i].Path
	}
	return paths
}

// Entries reads the registered projects with their names. Names are the
// authority for the workspaces-root segment a project's worktrees live
// under; duplicate or unsafe names in a legacy file are repaired
// deterministically in memory (first registration keeps the name, later
// collisions get -2, -3, … suffixes) and persisted by the next mutating
// save. Load stays read-only — it never repairs the primary file directly.
func (r *Registry) Entries() ([]RegistryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lockFile, err := lockRegistryFile(r.lockPath(), true)
	if err != nil {
		return nil, err
	}
	defer unlockRegistryFile(lockFile)

	// Load is read-only and should not repair the primary file directly.
	entries, _, err := r.loadUnlockedWithRecovery()
	return entries, err
}

// Save writes the project paths to the registry file. Names for paths that
// already carry one are preserved; paths without a stored name (or with an
// unsafe one) get a freshly minted, collision-free name — never a silent
// basename regeneration, which is what let two same-basename repos share one
// workspaces-root segment.
func (r *Registry) Save(paths []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	current, _, loadErr := r.loadUnlockedWithRecovery()
	if loadErr != nil {
		return loadErr
	}
	entries := rebindEntries(current, paths)
	return r.saveUnlocked(entries)
}

// rebindEntries rebuilds the entry list for paths: existing entries keep
// their name, new paths get a minted one, and dropped paths drop their
// entries. Order follows paths; names never collide (EqualFold) afterward.
func rebindEntries(current []RegistryEntry, paths []string) []RegistryEntry {
	byPath := make(map[string]RegistryEntry, len(current))
	for _, entry := range current {
		if canonicalProjectPath(entry.Path) != "" {
			byPath[canonicalProjectPath(entry.Path)] = entry
		}
	}
	taken := make(map[string]struct{}, len(paths)+len(current))
	for _, entry := range current {
		taken[strings.ToLower(entry.Name)] = struct{}{}
	}
	entries := make([]RegistryEntry, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		canonical := canonicalProjectPath(path)
		if canonical == "" {
			continue
		}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		entry, ok := byPath[canonical]
		if !ok || !isSafeProjectName(entry.Name) {
			entry = RegistryEntry{
				Name: mintProjectName(canonical, taken),
				Path: canonical,
			}
			taken[strings.ToLower(entry.Name)] = struct{}{}
		} else {
			entry.Path = canonical
		}
		entries = append(entries, entry)
	}
	return entries
}

func (r *Registry) loadUnlockedWithRecovery() ([]RegistryEntry, bool, error) {
	data, err := readRegistryFile(r.path)
	if os.IsNotExist(err) {
		backupPath := r.backupPath()
		backupData, backupErr := readRegistryFile(backupPath)
		if os.IsNotExist(backupErr) {
			return []RegistryEntry{}, false, nil
		}
		if backupErr != nil {
			return nil, false, backupErr
		}
		entries, parseErr := parseRegistryData(backupData, backupPath)
		if parseErr != nil {
			return nil, false, parseErr
		}
		return entries, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	entries, parseErr := parseRegistryData(data, r.path)
	if parseErr == nil {
		return entries, false, nil
	}
	if errors.Is(parseErr, ErrUnsupportedSchemaVersion) {
		// A newer-schema primary is intact data from a newer binary, not
		// corruption — refuse rather than recover a stale backup over it.
		return nil, false, parseErr
	}

	backupPath := r.backupPath()
	backupData, backupErr := readRegistryFile(backupPath)
	if backupErr != nil {
		return nil, false, errors.Join(parseErr, fmt.Errorf("read backup %s: %w", backupPath, backupErr))
	}
	backupEntries, backupParseErr := parseRegistryData(backupData, backupPath)
	if backupParseErr != nil {
		return nil, false, errors.Join(parseErr, fmt.Errorf("parse backup %s: %w", backupPath, backupParseErr))
	}
	return backupEntries, true, nil
}

func readRegistryFile(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(filepath.Base(path))
	closeErr := root.Close()
	if readErr != nil {
		if closeErr != nil {
			return nil, errors.Join(readErr, fmt.Errorf("close registry directory: %w", closeErr))
		}
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close registry directory: %w", closeErr)
	}
	return data, nil
}

func (r *Registry) saveUnlocked(entries []RegistryEntry) error {
	entries = normalizeAndDedupeEntries(entries)
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// Build registry structure
	registry := registryFile{
		Version:  registryFileVersion,
		Projects: entries,
	}

	return fsatomic.WriteJSON(r.path, registry)
}

// AddProject adds a project path to the registry. The project's name is the
// repo basename when that is free, and basename-2, basename-3, … when another
// registered project already holds it (registration order decides which repo
// keeps the bare basename).
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

	entries, recoveredFromBackup, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return err
	}

	// Check if already exists
	for _, entry := range entries {
		if canonicalProjectPath(entry.Path) == path {
			if recoveredFromBackup {
				return r.saveUnlocked(entries)
			}
			return nil // Already registered
		}
	}

	taken := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		taken[strings.ToLower(entry.Name)] = struct{}{}
	}
	entries = append(entries, RegistryEntry{
		Name: mintProjectName(path, taken),
		Path: path,
	})
	return r.saveUnlocked(entries)
}

// RenameProject renames the project registered at path. The new name becomes
// the authoritative workspaces-root segment for the project's future
// workspaces; existing worktrees keep working through the path-basename
// aliases the service accepts. The name must be a safe single path segment
// (validated here so every caller shares one rule) and must not collide with
// another registered project's name (case-insensitive, matching the
// case-insensitive filesystems the workspaces root commonly lives on).
func (r *Registry) RenameProject(path, newName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path = canonicalProjectPath(path)
	if path == "" {
		return errors.New("project path is required")
	}
	newName = strings.TrimSpace(newName)
	if err := validation.ValidateProjectName(newName); err != nil {
		return err
	}

	lockFile, err := lockRegistryFile(r.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	entries, _, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return err
	}

	renamed := false
	for i := range entries {
		if canonicalProjectPath(entries[i].Path) != path {
			continue
		}
		for j, other := range entries {
			if i == j {
				continue
			}
			if strings.EqualFold(other.Name, newName) {
				return fmt.Errorf("project name %q is already used by %s", newName, other.Path)
			}
		}
		entries[i].Name = newName
		renamed = true
		break
	}
	if !renamed {
		return fmt.Errorf("project %s is not registered", path)
	}
	return r.saveUnlocked(entries)
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

	entries, recoveredFromBackup, err := r.loadUnlockedWithRecovery()
	if err != nil {
		return err
	}

	// Filter out the path
	kept := make([]RegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if canonicalProjectPath(entry.Path) != path {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(entries) {
		if recoveredFromBackup {
			return r.saveUnlocked(entries)
		}
		return nil
	}

	return r.saveUnlocked(kept)
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

func parseRegistryData(data []byte, path string) ([]RegistryEntry, error) {
	var registry registryFile
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if registry.Version > registryFileVersion {
		return nil, fmt.Errorf("parse %s: unsupported schema version %d (newest known: %d): %w", path, registry.Version, registryFileVersion, ErrUnsupportedSchemaVersion)
	}

	// Repair names deterministically: a missing or unsafe name falls back to
	// the repo basename; duplicate names (pre-name-authority files could
	// write two repos with the same basename) keep the first registration
	// and suffix later ones. The repaired list persists on the next
	// mutating save.
	entries := make([]RegistryEntry, 0, len(registry.Projects))
	taken := make(map[string]struct{}, len(registry.Projects))
	for _, p := range registry.Projects {
		canonical := canonicalProjectPath(p.Path)
		if canonical == "" {
			continue
		}
		name := strings.TrimSpace(p.Name)
		if !isSafeProjectName(name) || collisionIn(taken, name) {
			name = mintProjectName(canonical, taken)
		}
		taken[strings.ToLower(name)] = struct{}{}
		entries = append(entries, RegistryEntry{Name: name, Path: canonical})
	}
	return entries, nil
}

// canonicalProjectPath, normalizeAndDedupeEntries, and the name
// minting/safety helpers live in registry_names.go — one file for the
// registry's name law.
