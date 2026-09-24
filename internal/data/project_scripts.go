package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// ProjectScriptStore persists user-level per-project script defaults — the
// layer that sits beneath each workspace's own Scripts and beneath the
// repo's trust-gated .amux/workspaces.json (precedence: repo → workspace →
// project; see plans/120). It is user-owned state: no trust gate, and a
// repo can never read into it — same contract as ProjectEnvStore.
//
// The backing file is a single versioned JSON envelope keyed by normalized
// repo path — sibling to project-env.json in ~/.amux — read on demand so
// edits apply without a create-time snapshot.
type ProjectScriptStore struct {
	path string
	mu   sync.Mutex
}

// NewProjectScriptStore returns a store whose backing file is
// dir/project-scripts.json.
func NewProjectScriptStore(dir string) *ProjectScriptStore {
	return &ProjectScriptStore{path: filepath.Join(dir, "project-scripts.json")}
}

// Path exposes the backing file path for diagnostics.
func (s *ProjectScriptStore) Path() string { return s.path }

// ForRepo returns the project's script defaults (normalized key); the zero
// ScriptsConfig when the project has none or the file is unreadable — a
// corrupt or missing file must never block script execution.
func (s *ProjectScriptStore) ForRepo(repoPath string) ScriptsConfig {
	key := NormalizePath(repoPath)
	if key == "" {
		return ScriptsConfig{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return ScriptsConfig{}
	}
	return all[key]
}

// Set replaces the project's script defaults and persists them atomically.
// An all-empty ScriptsConfig removes the entry rather than storing a
// useless empty object — an empty layer means absence, not override-to-empty
// (see plan 120's empty-layer semantics).
func (s *ProjectScriptStore) Set(repoPath string, scripts ScriptsConfig) error {
	key := NormalizePath(repoPath)
	if key == "" {
		return errors.New("project repo path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		// A corrupt file is replaced wholesale — Set is the authoritative
		// write and holding onto unparseable bytes would wedge every future
		// edit.
		all = map[string]ScriptsConfig{}
	}
	if scripts == (ScriptsConfig{}) {
		delete(all, key)
	} else {
		all[key] = scripts
	}
	return fsatomic.WriteJSON(s.path, projectScriptsFile{
		Version: projectScriptsFileVersion,
		Scripts: all,
	})
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

func (s *ProjectScriptStore) load() (map[string]ScriptsConfig, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]ScriptsConfig{}, nil
		}
		return nil, err
	}
	var file projectScriptsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Version > projectScriptsFileVersion {
		return nil, fmt.Errorf("unsupported project-scripts.json schema version %d (newest known: %d)", file.Version, projectScriptsFileVersion)
	}
	if file.Scripts == nil {
		return map[string]ScriptsConfig{}, nil
	}
	return file.Scripts, nil
}
