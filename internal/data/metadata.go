package data

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Metadata stores per-worktree configuration
type Metadata struct {
	Name                 string            `json:"name"`
	Branch               string            `json:"branch"`
	Repo                 string            `json:"repo"`
	Base                 string            `json:"base"`
	Created              string            `json:"created"`
	Assistant            string            `json:"assistant"`    // "claude", "codex", "gemini"
	Scripts              ScriptsConfig     `json:"scripts"`
	ScriptMode           string            `json:"script_mode"`  // "concurrent" or "nonconcurrent"
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

// MetadataStore manages per-worktree metadata persistence
type MetadataStore struct {
	metadataRoot string
}

// NewMetadataStore creates a new metadata store
func NewMetadataStore(metadataRoot string) *MetadataStore {
	return &MetadataStore{
		metadataRoot: metadataRoot,
	}
}

// metadataPath returns the path to the metadata file for a worktree
func (s *MetadataStore) metadataPath(wt *Worktree) string {
	return filepath.Join(s.metadataRoot, string(wt.ID()), "worktree.json")
}

// Load loads metadata for a worktree
func (s *MetadataStore) Load(wt *Worktree) (*Metadata, error) {
	path := s.metadataPath(wt)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default metadata if file doesn't exist
			return &Metadata{
				Name:       wt.Name,
				Branch:     wt.Branch,
				Repo:       wt.Repo,
				Base:       wt.Base,
				Created:    wt.Created.Format("2006-01-02T15:04:05Z07:00"),
				Assistant:  "claude",
				ScriptMode: "nonconcurrent",
				Env:        make(map[string]string),
			}, nil
		}
		return nil, err
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// Save saves metadata for a worktree
func (s *MetadataStore) Save(wt *Worktree, meta *Metadata) error {
	path := s.metadataPath(wt)
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

// Delete removes metadata for a worktree
func (s *MetadataStore) Delete(wt *Worktree) error {
	dir := filepath.Join(s.metadataRoot, string(wt.ID()))
	return os.RemoveAll(dir)
}
