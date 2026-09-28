package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// ProjectEnvStore persists user-level per-project environment variables —
// the layer that sits above repo `env` (trust-gated) and beneath each
// workspace's own Env map. It is user-owned state (like ws.Env and
// ws.Scripts): no trust gate, and the right home for secrets because nothing
// here is committed to a repository.
//
// The backing file is a single JSON map keyed by normalized repo path —
// sibling to trusted-scripts.json in ~/.amux — read on every spawn by the
// injected resolver so edits apply to existing workspaces without a
// create-time snapshot.
type ProjectEnvStore struct {
	path string
	mu   sync.Mutex
}

// NewProjectEnvStore returns a store whose backing file is dir/project-env.json.
func NewProjectEnvStore(dir string) *ProjectEnvStore {
	return &ProjectEnvStore{path: filepath.Join(dir, "project-env.json")}
}

// Path exposes the backing file path for diagnostics.
func (s *ProjectEnvStore) Path() string { return s.path }

// ForRepo returns a copy of the project's env map (normalized key); empty
// when the project has no stored map or the file is unreadable — a corrupt
// or missing file must never block script execution.
func (s *ProjectEnvStore) ForRepo(repoPath string) map[string]string {
	key := NormalizePath(repoPath)
	if key == "" {
		return map[string]string{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(all[key]))
	maps.Copy(out, all[key])
	return out
}

// Set replaces the project's env map and persists it atomically. An empty or
// nil map removes the entry rather than storing a useless empty object.
func (s *ProjectEnvStore) Set(repoPath string, env map[string]string) error {
	key := NormalizePath(repoPath)
	if key == "" {
		return errors.New("project repo path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		if errors.Is(err, ErrUnsupportedSchemaVersion) {
			// A newer-schema file is valid data from a newer binary —
			// refuse the write rather than clobbering it at our version.
			return err
		}
		// A corrupt file is replaced wholesale — Set is the authoritative
		// write and holding onto unparseable bytes would wedge every future
		// edit.
		all = map[string]map[string]string{}
	}
	if len(env) == 0 {
		delete(all, key)
	} else {
		entry := make(map[string]string, len(env))
		maps.Copy(entry, env)
		all[key] = entry
	}
	return fsatomic.WriteJSON(s.path, projectEnvFile{
		Version: projectEnvFileVersion,
		Env:     all,
	})
}

// projectEnvFileVersion is the newest project-env.json schema written and
// readable. v0 = the pre-versioning bare map shape.
const projectEnvFileVersion = 1

// projectEnvFile is the versioned envelope written since v1. Repo keys are
// normalized absolute paths, so a top-level "version" key can never collide
// with a v0 entry — its presence means envelope.
type projectEnvFile struct {
	Version int                          `json:"version"`
	Env     map[string]map[string]string `json:"env"`
}

func (s *ProjectEnvStore) load() (map[string]map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]map[string]string{}, nil
		}
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if _, isEnvelope := probe["version"]; isEnvelope {
		var file projectEnvFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, err
		}
		if file.Version > projectEnvFileVersion {
			return nil, fmt.Errorf("unsupported project-env.json schema version %d (newest known: %d): %w", file.Version, projectEnvFileVersion, ErrUnsupportedSchemaVersion)
		}
		if file.Env == nil {
			return map[string]map[string]string{}, nil
		}
		return file.Env, nil
	}
	// v0: bare map shape.
	out := map[string]map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
