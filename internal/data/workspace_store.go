package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

const workspaceFilename = "workspace.json"

// WorkspaceStore manages workspace persistence
type WorkspaceStore struct {
	root string // ~/.amux/workspaces
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
	}

	// Apply defaults for missing fields
	if ws.Assistant == "" {
		ws.Assistant = "claude"
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	if ws.Env == nil {
		ws.Env = make(map[string]string)
	}

	return ws, nil
}

// Save saves a workspace to the store using atomic write
func (s *WorkspaceStore) Save(ws *Workspace) error {
	id := ws.ID()
	path := s.workspacePath(id)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename for atomic operation
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file on rename failure
		return err
	}
	return nil
}

// Delete removes a workspace from the store
func (s *WorkspaceStore) Delete(id WorkspaceID) error {
	dir := filepath.Join(s.root, string(id))
	return os.RemoveAll(dir)
}

// ListByRepo returns all workspaces for a given repository path
func (s *WorkspaceStore) ListByRepo(repoPath string) ([]*Workspace, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}

	var workspaces []*Workspace
	var loadErrors int
	for _, id := range ids {
		ws, err := s.Load(id)
		if err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, err)
			loadErrors++
			continue
		}
		// Skip legacy workspaces with empty Root - they are handled via
		// LoadMetadataFor which matches by ID computed from discovered Root/Repo.
		// Including them here would create duplicates since their computed ID
		// (from empty Root) differs from the stored directory name.
		if ws.Root == "" {
			continue
		}
		if ws.Repo == repoPath {
			workspaces = append(workspaces, ws)
		}
	}

	// If we had workspace IDs but couldn't load any, surface the error
	// so callers can distinguish between "no workspaces" and "data corruption"
	if loadErrors > 0 && len(workspaces) == 0 && len(ids) > 0 {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s", loadErrors, repoPath)
	}

	return workspaces, nil
}

// LoadMetadataFor loads stored metadata for a workspace and merges it into the provided workspace.
// This handles legacy metadata files that don't have Root/Repo fields by using the workspace's
// computed ID (based on Repo+Root from git discovery).
// Returns (true, nil) if metadata was found and merged.
// Returns (false, nil) if no metadata file exists (safe to apply defaults).
// Returns (false, err) if metadata exists but couldn't be read (don't overwrite).
func (s *WorkspaceStore) LoadMetadataFor(ws *Workspace) (bool, error) {
	id := ws.ID()
	stored, err := s.Load(id)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // No metadata file, safe to apply defaults
		}
		return false, err // Read/unmarshal error, don't overwrite
	}

	// Merge stored metadata into workspace, preserving discovered git info
	// (Root, Repo, Name, Branch are from git discovery and should be kept)
	ws.Created = stored.Created
	ws.Base = stored.Base
	ws.Runtime = stored.Runtime
	ws.Assistant = stored.Assistant
	ws.Scripts = stored.Scripts
	ws.ScriptMode = stored.ScriptMode
	ws.Env = stored.Env
	ws.OpenTabs = stored.OpenTabs
	ws.ActiveTabIndex = stored.ActiveTabIndex

	// Apply defaults if stored metadata had empty values (legacy files)
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

	return true, nil
}

// workspaceJSON is used for loading old-format metadata files during migration
type workspaceJSON struct {
	Name           string            `json:"name"`
	Branch         string            `json:"branch"`
	Repo           string            `json:"repo"`
	Base           string            `json:"base"`
	Root           string            `json:"root"`
	Created        json.RawMessage   `json:"created"` // Can be time.Time or string
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
