package data

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Metadata stores per-workspace configuration
type Metadata struct {
	Name                 string            `json:"name"`
	Branch               string            `json:"branch"`
	Repo                 string            `json:"repo"`
	Base                 string            `json:"base"`
	Created              string            `json:"created"`
	Assistant            string            `json:"assistant"` // "claude", "codex", "gemini"
	Runtime              string            `json:"runtime"`   // "local-worktree", "local-docker", "cloud-sandbox" (default: "local-worktree")
	Scripts              ScriptsConfig     `json:"scripts"`
	ScriptMode           string            `json:"script_mode"` // "concurrent" or "nonconcurrent"
	Env                  map[string]string `json:"env"`
	PortBase             *int              `json:"port_base"`
	LastActiveBufferName string            `json:"last_active_buffer_name"`
	OpenTabs             []TabInfo         `json:"open_tabs,omitempty"` // Persisted tabs
	ActiveTabIndex       int               `json:"active_tab_index"`
}

// TabInfo stores information about an open tab
type TabInfo struct {
	Assistant string `json:"assistant"`
	Name      string `json:"name"`
}

// ScriptsConfig holds the setup/run/archive script commands
type ScriptsConfig struct {
	Setup   string `json:"setup"`
	Run     string `json:"run"`
	Archive string `json:"archive"`
}

// LoadResult includes metadata and any migration warnings
type LoadResult struct {
	Metadata *Metadata
	Warning  string // Non-empty if migration action needed
}

// MetadataStore manages per-workspace metadata persistence
type MetadataStore struct {
	metadataRoot string
}

// NewMetadataStore creates a new metadata store
func NewMetadataStore(metadataRoot string) *MetadataStore {
	return &MetadataStore{
		metadataRoot: metadataRoot,
	}
}

const (
	metadataFilename       = "workspace.json"
	legacyMetadataFilename = "worktree.json" // pre-v1.0 filename
)

// metadataPath returns the path to the metadata file for a workspace (used for saving)
func (s *MetadataStore) metadataPath(ws *Workspace) string {
	return filepath.Join(s.metadataRoot, string(ws.ID()), metadataFilename)
}

// Load loads metadata for a workspace
func (s *MetadataStore) Load(ws *Workspace) (*LoadResult, error) {
	dir := filepath.Join(s.metadataRoot, string(ws.ID()))
	newPath := filepath.Join(dir, metadataFilename)
	legacyPath := filepath.Join(dir, legacyMetadataFilename)

	// Try new file first
	data, readErr := os.ReadFile(newPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		// Real read error (permission, I/O) - return it
		return nil, readErr
	}
	if readErr == nil {
		var meta Metadata
		unmarshalErr := json.Unmarshal(data, &meta)
		if unmarshalErr == nil {
			return &LoadResult{Metadata: &meta}, nil
		}
		// New file corrupted, try legacy as fallback
		if legacyData, legacyErr := os.ReadFile(legacyPath); legacyErr == nil {
			var legacyMeta Metadata
			if err := json.Unmarshal(legacyData, &legacyMeta); err != nil {
				return nil, err // Both corrupted
			}
			return &LoadResult{
				Metadata: &legacyMeta,
				Warning:  "workspace.json was corrupted; recovered from worktree.json. Please save to recreate workspace.json.",
			}, nil
		}
		// New file corrupted, no legacy - return error
		return nil, unmarshalErr
	}

	// Try legacy file
	legacyData, legacyErr := os.ReadFile(legacyPath)
	if legacyErr != nil && !os.IsNotExist(legacyErr) {
		// Real read error (permission, I/O) - return it
		return nil, legacyErr
	}
	if legacyErr == nil {
		var meta Metadata
		if err := json.Unmarshal(legacyData, &meta); err != nil {
			return nil, err
		}
		return &LoadResult{
			Metadata: &meta,
			Warning:  "Using legacy metadata file. Please rename " + legacyMetadataFilename + " to " + metadataFilename,
		}, nil
	}

	// Neither exists, return defaults
	return &LoadResult{
		Metadata: &Metadata{
			Name:       ws.Name,
			Branch:     ws.Branch,
			Repo:       ws.Repo,
			Base:       ws.Base,
			Created:    ws.Created.Format("2006-01-02T15:04:05Z07:00"),
			Assistant:  "claude",
			Runtime:    "local-worktree",
			ScriptMode: "nonconcurrent",
			Env:        make(map[string]string),
		},
	}, nil
}

// Save saves metadata for a workspace
func (s *MetadataStore) Save(ws *Workspace, meta *Metadata) error {
	path := s.metadataPath(ws)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Delete removes metadata for a workspace
func (s *MetadataStore) Delete(ws *Workspace) error {
	dir := filepath.Join(s.metadataRoot, string(ws.ID()))
	return os.RemoveAll(dir)
}
