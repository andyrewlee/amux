package data

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// ProjectScriptStore persists user-level per-project script defaults — the
// layer that sits beneath each workspace's own Scripts and beneath the
// repo's trust-gated .amux/workspaces.json (precedence: repo → workspace →
// project). It is user-owned state: no trust gate, and a
// repo can never read into it — same contract as ProjectEnvStore.
//
// The backing file is a single versioned JSON envelope keyed by normalized
// repo path — sibling to project-env.json in ~/.amux — read on demand so
// edits apply without a create-time snapshot. Load/mutate/flock/atomic-write
// mechanics are shared with the other per-project envelope stores via
// projectEnvelope; the wire shape and empty-record rule below are this
// store's own.
type ProjectScriptStore struct {
	*projectEnvelope[ScriptsConfig]
}

// NewProjectScriptStore returns a store whose backing file is
// dir/project-scripts.json.
func NewProjectScriptStore(dir string) *ProjectScriptStore {
	return &ProjectScriptStore{&projectEnvelope[ScriptsConfig]{
		path: filepath.Join(dir, "project-scripts.json"),
		decodeEnvelope: func(raw []byte) (map[string]ScriptsConfig, error) {
			var file projectScriptsFile
			if err := json.Unmarshal(raw, &file); err != nil {
				return nil, err
			}
			if file.Version > projectScriptsFileVersion {
				return nil, fmt.Errorf("unsupported project-scripts.json schema version %d (newest known: %d): %w", file.Version, projectScriptsFileVersion, ErrUnsupportedSchemaVersion)
			}
			if file.Scripts == nil {
				return map[string]ScriptsConfig{}, nil
			}
			return file.Scripts, nil
		},
		encodeEnvelope: func(all map[string]ScriptsConfig) any {
			return projectScriptsFile{Version: projectScriptsFileVersion, Scripts: all}
		},
		emptyEntry: func(v ScriptsConfig) bool { return v == (ScriptsConfig{}) },
	}}
}

// ForRepo returns the project's script defaults (normalized key); the zero
// ScriptsConfig when the project has none or the file is unreadable — a
// corrupt or missing file must never block script execution.
func (s *ProjectScriptStore) ForRepo(repoPath string) ScriptsConfig {
	return s.forRepo(repoPath, ScriptsConfig{})
}

// Set replaces the project's script defaults and persists them atomically.
// An all-empty ScriptsConfig removes the entry rather than storing a
// useless empty object — an empty layer means absence, not override-to-empty.
func (s *ProjectScriptStore) Set(repoPath string, scripts ScriptsConfig) error {
	return s.set(repoPath, scripts)
}

// projectScriptsFileVersion is the newest project-scripts.json schema
// written and readable. Unlike project-env.json there is no v0: the file
// shipped with the envelope already in place.
const projectScriptsFileVersion = 1

// projectScriptsFile is the versioned envelope. Repo keys are normalized
// absolute paths, so the top-level "version"/"scripts" keys can never
// collide with an entry.
type projectScriptsFile struct {
	Version int                      `json:"version"`
	Scripts map[string]ScriptsConfig `json:"scripts"`
}
