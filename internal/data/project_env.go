package data

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
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
// create-time snapshot. Load/mutate/flock/atomic-write mechanics are shared
// with the other per-project envelope stores via projectEnvelope; the wire
// shape and empty-record rule below are this store's own.
type ProjectEnvStore struct {
	*projectEnvelope[map[string]string]
}

// NewProjectEnvStore returns a store whose backing file is dir/project-env.json.
func NewProjectEnvStore(dir string) *ProjectEnvStore {
	return &ProjectEnvStore{&projectEnvelope[map[string]string]{
		path: filepath.Join(dir, "project-env.json"),
		decodeEnvelope: func(raw []byte) (map[string]map[string]string, error) {
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
			if out == nil {
				// A valid top-level `null` decodes to nil — treat it as
				// empty state like the v1 envelope's nil-Env branch so a
				// later Set can assign.
				return map[string]map[string]string{}, nil
			}
			return out, nil
		},
		encodeEnvelope: func(all map[string]map[string]string) any {
			return projectEnvFile{Version: projectEnvFileVersion, Env: all}
		},
		emptyEntry: func(v map[string]string) bool { return len(v) == 0 },
		copyRecord: func(v map[string]string) map[string]string {
			out := make(map[string]string, len(v))
			maps.Copy(out, v)
			return out
		},
	}}
}

// ForRepo returns a copy of the project's env map (normalized key); empty
// when the project has no stored map or the file is unreadable — a corrupt
// or missing file must never block script execution.
func (s *ProjectEnvStore) ForRepo(repoPath string) map[string]string {
	return s.forRepo(repoPath, map[string]string{})
}

// Set replaces the project's env map and persists it atomically. An empty or
// nil map removes the entry rather than storing a useless empty object.
func (s *ProjectEnvStore) Set(repoPath string, env map[string]string) error {
	return s.set(repoPath, env)
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
