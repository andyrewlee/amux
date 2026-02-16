package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const workspaceFilename = "workspace.json"

// WorkspaceStore manages workspace persistence
type WorkspaceStore struct {
	root             string // ~/.amux/workspaces-metadata
	defaultAssistant string
}

// NewWorkspaceStore creates a new workspace store
func NewWorkspaceStore(root string) *WorkspaceStore {
	return &WorkspaceStore{
		root:             root,
		defaultAssistant: DefaultAssistant,
	}
}

// SetDefaultAssistant updates the assistant used when applying defaults while loading metadata.
func (s *WorkspaceStore) SetDefaultAssistant(name string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		s.defaultAssistant = DefaultAssistant
		return
	}
	s.defaultAssistant = trimmed
}

// ResolvedDefaultAssistant returns the configured default assistant,
// falling back to DefaultAssistant if none is set.
func (s *WorkspaceStore) ResolvedDefaultAssistant() string {
	if s == nil {
		return DefaultAssistant
	}
	name := strings.TrimSpace(s.defaultAssistant)
	if name == "" {
		return DefaultAssistant
	}
	return name
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
	return s.load(id, true)
}

func (s *WorkspaceStore) load(id WorkspaceID, applyDefaults bool) (*Workspace, error) {
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

	if applyDefaults {
		// Apply defaults for missing fields.
		s.applyWorkspaceDefaults(ws)
	}

	return ws, nil
}

// Save saves a workspace to the store using atomic write
func (s *WorkspaceStore) Save(ws *Workspace) error {
	if err := validateWorkspaceForSave(ws); err != nil {
		return err
	}
	id := ws.ID()
	oldID := ws.storeID
	if oldID == id {
		oldID = ""
	}
	if oldID != "" {
		if err := validateWorkspaceID(oldID); err != nil {
			logging.Warn("Skipping cleanup for invalid old workspace metadata id %q: %v", oldID, err)
			oldID = ""
		}
	}
	path := s.workspacePath(id)
	dir := filepath.Dir(path)

	lockFiles, err := s.lockWorkspaceIDs(id, oldID)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename for atomic operation
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file on rename failure
		return err
	}
	if oldID != "" {
		if err := s.deleteWorkspaceDir(oldID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", oldID, err)
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
	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)
	return s.deleteWorkspaceDir(id)
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
	//
	// Merge hierarchy: stored non-empty values win → caller's pre-set values
	// are preserved for empty stored fields → applyWorkspaceDefaults fills
	// any remaining gaps. The conditional "if stored.X != ''" guards below
	// implement the first two tiers of this hierarchy.
	if stored.Name != "" {
		ws.Name = stored.Name
	}
	ws.Created = stored.Created
	ws.Base = stored.Base
	ws.Runtime = stored.Runtime
	if stored.Assistant != "" {
		ws.Assistant = stored.Assistant
	}
	ws.Scripts = stored.Scripts
	ws.ScriptMode = stored.ScriptMode
	ws.Env = stored.Env
	ws.OpenTabs = stored.OpenTabs
	ws.ActiveTabIndex = stored.ActiveTabIndex
	ws.Archived = stored.Archived
	ws.ArchivedAt = stored.ArchivedAt
	ws.storeID = stored.storeID

	// Apply defaults if stored metadata had empty values
	s.applyWorkspaceDefaults(ws)

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
		s.applyWorkspaceDefaults(discovered)
		return s.Save(discovered)
	}

	merged := *stored
	merged.Repo = discovered.Repo
	merged.Root = discovered.Root
	merged.Branch = discovered.Branch
	if merged.Name == "" {
		merged.Name = discovered.Name
	}
	if merged.Assistant == "" {
		merged.Assistant = discovered.Assistant
	}
	if merged.Created.IsZero() && !discovered.Created.IsZero() {
		merged.Created = discovered.Created
	}
	merged.Archived = false
	merged.ArchivedAt = time.Time{}
	s.applyWorkspaceDefaults(&merged)

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

func (s *WorkspaceStore) applyWorkspaceDefaults(ws *Workspace) {
	if ws.Assistant == "" {
		ws.Assistant = s.ResolvedDefaultAssistant()
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

func (s *WorkspaceStore) findStoredWorkspace(repo, root string) (*Workspace, WorkspaceID, error) {
	canonicalID := Workspace{Repo: repo, Root: root}.ID()
	// load with applyDefaults=false so raw stored values are visible for merge
	// logic — empty fields indicate "not set" and influence precedence decisions.
	ws, err := s.load(canonicalID, false)
	if err == nil {
		return ws, canonicalID, nil
	}
	if !os.IsNotExist(err) {
		return nil, "", err
	}
	targetRepo, targetRoot := canonicalLookupPath(repo), canonicalLookupPath(root)
	if targetRepo == "" || targetRoot == "" {
		return nil, "", nil
	}
	// Fallback: scan all workspaces for a resolved repo+root match (O(n), rare).
	ids, err := s.List()
	if err != nil {
		return nil, "", err
	}
	var bestWS *Workspace
	var bestID WorkspaceID
	for _, id := range ids {
		// applyDefaults=false: see comment on first load call above.
		candidate, err := s.load(id, false)
		if err != nil {
			if !os.IsNotExist(err) {
				logging.Warn("Skipping unreadable workspace metadata %s during fallback lookup: %v", id, err)
			}
			continue
		}
		if canonicalLookupPath(candidate.Repo) != targetRepo || canonicalLookupPath(candidate.Root) != targetRoot {
			continue
		}
		if shouldPreferWorkspace(candidate, bestWS) {
			bestWS = candidate
			bestID = id
		}
	}
	return bestWS, bestID, nil
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
	var unknownLoadErrors int
	for _, id := range ids {
		ws, err := s.Load(id)
		if err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, err)
			loadErrors++
			if repo, ok := s.repoHintForWorkspaceID(id); ok {
				if canonicalLookupPath(repo) == targetRepo {
					targetLoadErrors++
				}
			} else {
				unknownLoadErrors++
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
	if unknownLoadErrors > 0 && len(workspaces) == 0 {
		return nil, fmt.Errorf("failed to load %d workspace(s) with unreadable repo for %s", unknownLoadErrors, repoPath)
	}
	if loadErrors > 0 && len(workspaces) == 0 && loadErrors == len(ids) {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s", loadErrors, repoPath)
	}

	return workspaces, nil
}

// validateWorkspaceID rejects IDs containing "..", "/", or "\" to prevent path
// traversal attacks — workspace IDs are used to construct filesystem paths.
// This is intentionally stricter than the WorkspaceID type itself.
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
