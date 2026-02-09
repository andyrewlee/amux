package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const workspaceFilename = "workspace.json"

var repoFieldHintPattern = regexp.MustCompile(`"repo"\s*:\s*"([^"]*)"`)

// WorkspaceStore manages workspace persistence
type WorkspaceStore struct {
	root string // ~/.amux/workspaces-metadata
}

// NewWorkspaceStore creates a new workspace store
func NewWorkspaceStore(root string) *WorkspaceStore {
	return &WorkspaceStore{
		root: root,
	}
}

// workspacePath returns the path to the workspace file for a workspace ID
func (s *WorkspaceStore) workspacePath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id), workspaceFilename)
}

func (s *WorkspaceStore) workspaceLockPath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id)+".lock")
}

// List returns all workspace IDs stored in the store
func (s *WorkspaceStore) List() ([]WorkspaceID, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var ids []WorkspaceID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check if workspace.json exists in this directory
		wsPath := filepath.Join(s.root, entry.Name(), workspaceFilename)
		if _, err := os.Stat(wsPath); err == nil {
			ids = append(ids, WorkspaceID(entry.Name()))
		}
	}
	return ids, nil
}

// Load loads a workspace by its ID
func (s *WorkspaceStore) Load(id WorkspaceID) (*Workspace, error) {
	if err := validateWorkspaceID(id); err != nil {
		return nil, err
	}
	path := s.workspacePath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Use workspaceJSON for backward-compatible loading
	var raw workspaceJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	ws := &Workspace{
		Name:           raw.Name,
		Branch:         raw.Branch,
		Base:           raw.Base,
		Repo:           raw.Repo,
		Root:           raw.Root,
		Created:        parseCreated(raw.Created),
		Runtime:        NormalizeRuntime(raw.Runtime),
		Assistant:      raw.Assistant,
		Scripts:        raw.Scripts,
		ScriptMode:     raw.ScriptMode,
		Env:            raw.Env,
		OpenTabs:       raw.OpenTabs,
		ActiveTabIndex: raw.ActiveTabIndex,
		Archived:       raw.Archived,
		ArchivedAt:     parseCreated(raw.ArchivedAt),
	}
	ws.storeID = id

	// Apply defaults for missing fields
	applyWorkspaceDefaults(ws)

	return ws, nil
}

// Save saves a workspace to the store using atomic write
func (s *WorkspaceStore) Save(ws *Workspace) error {
	if err := validateWorkspaceForSave(ws); err != nil {
		return err
	}
	id := ws.ID()
	path := s.workspacePath(id)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lockFile, err := lockRegistryFile(s.workspaceLockPath(id), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)

	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename for atomic operation
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file on rename failure
		return err
	}
	// Clean up the old metadata directory when the workspace ID changed (e.g.,
	// after a repo/root path change). This acquires a second lock for the old ID,
	// which is safe: the two IDs are distinct and no reverse lock ordering exists.
	if ws.storeID != "" && ws.storeID != id {
		if err := s.Delete(ws.storeID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", ws.storeID, err)
		}
	}
	ws.storeID = id
	return nil
}

// Delete removes a workspace from the store
func (s *WorkspaceStore) Delete(id WorkspaceID) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	lockFile, err := lockRegistryFile(s.workspaceLockPath(id), false)
	if err != nil {
		return err
	}
	defer unlockRegistryFile(lockFile)
	dir := filepath.Join(s.root, string(id))
	return os.RemoveAll(dir)
}

// ListByRepo returns all workspaces for a given repository path
func (s *WorkspaceStore) ListByRepo(repoPath string) ([]*Workspace, error) {
	return s.listByRepo(repoPath, false)
}

// LoadMetadataFor loads stored metadata for a workspace and merges it into the provided workspace.
// Uses the workspace's computed ID (based on Repo+Root) to find stored metadata.
// Returns (true, nil) if metadata was found and merged.
// Returns (false, nil) if no metadata file exists (safe to apply defaults).
// Returns (false, err) if metadata exists but couldn't be read (don't overwrite).
func (s *WorkspaceStore) LoadMetadataFor(ws *Workspace) (bool, error) {
	if ws == nil {
		return false, errors.New("workspace is required")
	}
	stored, _, err := s.findStoredWorkspace(ws.Repo, ws.Root)
	if err != nil {
		return false, err
	}
	if stored == nil {
		return false, nil // No metadata file, safe to apply defaults
	}

	// Merge stored metadata into workspace. Store owns metadata/UI state;
	// discovery only updates Root/Repo/Branch (and Name if stored is empty).
	if stored.Name != "" {
		ws.Name = stored.Name
	}
	ws.Created = stored.Created
	ws.Base = stored.Base
	ws.Runtime = stored.Runtime
	ws.Assistant = stored.Assistant
	ws.Scripts = stored.Scripts
	ws.ScriptMode = stored.ScriptMode
	ws.Env = stored.Env
	ws.OpenTabs = stored.OpenTabs
	ws.ActiveTabIndex = stored.ActiveTabIndex
	ws.Archived = stored.Archived
	ws.ArchivedAt = stored.ArchivedAt
	ws.storeID = stored.storeID

	// Apply defaults if stored metadata had empty values
	applyWorkspaceDefaults(ws)

	return true, nil
}

// UpsertFromDiscovery merges a discovered workspace into the store.
// Store metadata wins; discovery updates Repo/Root/Branch (and Name if empty).
// Archived state is cleared on discovery.
func (s *WorkspaceStore) UpsertFromDiscovery(discovered *Workspace) error {
	if discovered == nil {
		return nil
	}

	stored, storedID, err := s.findStoredWorkspace(discovered.Repo, discovered.Root)
	if err != nil {
		return err
	}

	if stored == nil {
		if discovered.Created.IsZero() {
			discovered.Created = time.Now()
		}
		applyWorkspaceDefaults(discovered)
		return s.Save(discovered)
	}

	merged := *stored
	merged.Repo = discovered.Repo
	merged.Root = discovered.Root
	merged.Branch = discovered.Branch
	if merged.Name == "" {
		merged.Name = discovered.Name
	}
	if merged.Created.IsZero() && !discovered.Created.IsZero() {
		merged.Created = discovered.Created
	}
	merged.Archived = false
	merged.ArchivedAt = time.Time{}
	applyWorkspaceDefaults(&merged)

	newID := merged.ID()
	if err := s.Save(&merged); err != nil {
		return err
	}
	if storedID != "" && storedID != newID {
		if err := s.Delete(storedID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", storedID, err)
		}
	}
	return nil
}

// workspaceJSON is used for loading old-format metadata files during migration
type workspaceJSON struct {
	Name           string            `json:"name"`
	Branch         string            `json:"branch"`
	Repo           string            `json:"repo"`
	Base           string            `json:"base"`
	Root           string            `json:"root"`
	Created        json.RawMessage   `json:"created"` // Can be time.Time or string
	Archived       bool              `json:"archived"`
	ArchivedAt     json.RawMessage   `json:"archived_at,omitempty"`
	Assistant      string            `json:"assistant"`
	Runtime        string            `json:"runtime"`
	Scripts        ScriptsConfig     `json:"scripts"`
	ScriptMode     string            `json:"script_mode"`
	Env            map[string]string `json:"env"`
	OpenTabs       []TabInfo         `json:"open_tabs,omitempty"`
	ActiveTabIndex int               `json:"active_tab_index"`
}

// parseCreated parses a created timestamp from either time.Time format or string format
func parseCreated(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}

	// Try parsing as time.Time first (JSON format)
	var t time.Time
	if err := json.Unmarshal(raw, &t); err == nil && !t.IsZero() {
		return t
	}

	// Try parsing as string (RFC3339 format from old metadata)
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return parsed
		}
	}

	return time.Time{}
}

func applyWorkspaceDefaults(ws *Workspace) {
	if ws.Assistant == "" {
		ws.Assistant = "claude"
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	if ws.Env == nil {
		ws.Env = make(map[string]string)
	}
	if ws.Runtime == "" {
		ws.Runtime = RuntimeLocalWorktree
	}
}

func shouldPreferWorkspace(candidate, existing *Workspace) bool {
	if existing == nil {
		return true
	}
	if candidate == nil {
		return false
	}
	if existing.Archived && !candidate.Archived {
		return true
	}
	if !existing.Archived && candidate.Archived {
		return false
	}
	if existing.Created.IsZero() && !candidate.Created.IsZero() {
		return true
	}
	if !existing.Created.IsZero() && candidate.Created.IsZero() {
		return false
	}
	if existing.Name == "" && candidate.Name != "" {
		return true
	}
	return false
}

func (s *WorkspaceStore) findStoredWorkspace(repo, root string) (*Workspace, WorkspaceID, error) {
	canonicalID := Workspace{Repo: repo, Root: root}.ID()
	ws, err := s.Load(canonicalID)
	if err == nil {
		return ws, canonicalID, nil
	}
	if !os.IsNotExist(err) {
		return nil, "", err
	}
	targetRepo := canonicalLookupPath(repo)
	targetRoot := canonicalLookupPath(root)
	if targetRepo == "" || targetRoot == "" {
		return nil, "", nil
	}
	// Fallback: scan all workspaces to find a match by resolved repo+root paths.
	// This is O(n) but only triggers when the canonical ID lookup above misses
	// (e.g., after a symlink change or path rename).
	ids, err := s.List()
	if err != nil {
		return nil, "", err
	}
	for _, id := range ids {
		candidate, err := s.Load(id)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", err
		}
		if canonicalLookupPath(candidate.Repo) != targetRepo {
			continue
		}
		if canonicalLookupPath(candidate.Root) != targetRoot {
			continue
		}
		return candidate, id, nil
	}
	return nil, "", nil
}

func (s *WorkspaceStore) ListByRepoIncludingArchived(repoPath string) ([]*Workspace, error) {
	return s.listByRepo(repoPath, true)
}

func (s *WorkspaceStore) listByRepo(repoPath string, includeArchived bool) ([]*Workspace, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}

	targetRepo := canonicalLookupPath(repoPath)
	var workspaces []*Workspace
	seen := make(map[string]int)
	var loadErrors int
	var targetLoadErrors int
	for _, id := range ids {
		ws, err := s.Load(id)
		if err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, err)
			loadErrors++
			if hintRepo, hintOK := s.repoHintForWorkspaceID(id); hintOK && canonicalLookupPath(hintRepo) == targetRepo {
				targetLoadErrors++
			}
			continue
		}
		if ws.Root == "" {
			logging.Warn("Skipping workspace %s with empty Root", id)
			continue
		}
		if !includeArchived && ws.Archived {
			continue
		}
		if canonicalLookupPath(ws.Repo) != targetRepo {
			continue
		}
		repoKey := canonicalLookupPath(ws.Repo)
		rootKey := canonicalLookupPath(ws.Root)
		key := workspaceIdentity(ws.Repo, ws.Root)
		if repoKey != "" && rootKey != "" {
			key = repoKey + "\n" + rootKey
		}
		if idx, ok := seen[key]; ok {
			if shouldPreferWorkspace(ws, workspaces[idx]) {
				workspaces[idx] = ws
			}
			continue
		}
		seen[key] = len(workspaces)
		workspaces = append(workspaces, ws)
	}

	if targetLoadErrors > 0 && len(workspaces) == 0 {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s", targetLoadErrors, repoPath)
	}
	// If we had workspace IDs but couldn't load any, surface the error
	// so callers can distinguish between "no workspaces" and "data corruption"
	if loadErrors > 0 && len(workspaces) == 0 && loadErrors == len(ids) {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s", loadErrors, repoPath)
	}

	return workspaces, nil
}

func validateWorkspaceID(id WorkspaceID) error {
	value := strings.TrimSpace(string(id))
	if value == "" {
		return errors.New("workspace id is required")
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("invalid workspace id %q", id)
	}
	return nil
}

func validateWorkspaceForSave(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	repo := NormalizePath(strings.TrimSpace(ws.Repo))
	if repo == "" {
		return errors.New("workspace repo is required")
	}
	root := NormalizePath(strings.TrimSpace(ws.Root))
	if root == "" {
		return errors.New("workspace root is required")
	}
	return nil
}

func (s *WorkspaceStore) repoHintForWorkspaceID(id WorkspaceID) (string, bool) {
	data, err := os.ReadFile(s.workspacePath(id))
	if err != nil {
		return "", false
	}
	var ws workspaceJSON
	if err := json.Unmarshal(data, &ws); err == nil && strings.TrimSpace(ws.Repo) != "" {
		return ws.Repo, true
	}
	match := repoFieldHintPattern.FindSubmatch(data)
	if len(match) < 2 {
		return "", false
	}
	raw := append([]byte{'"'}, match[1]...)
	raw = append(raw, '"')
	repo, err := strconv.Unquote(string(raw))
	if err != nil {
		return string(bytes.TrimSpace(match[1])), true
	}
	return repo, true
}
