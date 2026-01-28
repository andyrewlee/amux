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
	Runtime              string            `json:"runtime"`   // "local", "local-docker", "cloud-sandbox" (default: "local")
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

const metadataFilename = "workspace.json"

// metadataPath returns the path to the metadata file for a workspace (used for saving)
func (s *MetadataStore) metadataPath(ws *Workspace) string {
	return filepath.Join(s.metadataRoot, string(ws.ID()), metadataFilename)
}

// Load loads metadata for a workspace
func (s *MetadataStore) Load(ws *Workspace) (*Metadata, error) {
	metaPath := filepath.Join(s.metadataRoot, string(ws.ID()), metadataFilename)

	data, err := os.ReadFile(metaPath)
	if os.IsNotExist(err) {
		// File doesn't exist, return defaults
		return &Metadata{
			Name:       ws.Name,
			Branch:     ws.Branch,
			Repo:       ws.Repo,
			Base:       ws.Base,
			Created:    ws.Created.Format("2006-01-02T15:04:05Z07:00"),
			Assistant:  "claude",
			Runtime:    "local",
			ScriptMode: "nonconcurrent",
			Env:        make(map[string]string),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
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
