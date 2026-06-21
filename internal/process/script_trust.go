package process

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsatomic"
	"github.com/andyrewlee/amux/internal/logging"
)

// trustRegistryFilename is the basename of the per-user registry that records
// which repos' .amux/workspaces.json content the user has approved.
const trustRegistryFilename = "trusted-scripts.json"

// ScriptTrust is the per-user registry of repos whose .amux/workspaces.json
// content the user has explicitly approved. It maps a normalized repo path to
// the hex SHA-256 of the config content that was approved, so any later edit to
// the repo's config invalidates the approval (the hash no longer matches).
//
// The security property it enforces: repo-supplied executable config keys
// (config.SetupWorkspace / config.RunScript / config.ArchiveScript loaded from
// .amux/workspaces.json) never execute unless IsTrusted returns true. It is
// fail-closed — a missing or corrupt registry yields "not trusted". If a future
// change adds new repo-supplied executable config keys, they must go through the
// same IsTrusted check.
type ScriptTrust struct {
	path string
}

// NewScriptTrust returns a registry whose backing file lives in dir.
func NewScriptTrust(dir string) *ScriptTrust {
	return &ScriptTrust{path: filepath.Join(dir, trustRegistryFilename)}
}

// defaultScriptTrust returns a registry rooted at the amux home dir, resolved
// the same way internal/config resolves the data/config dir (no hardcoded
// ~/.amux). On any resolution error it falls back to a relative path so the
// registry simply never matches (fail-closed) rather than panicking at startup.
func defaultScriptTrust() *ScriptTrust {
	paths, err := config.DefaultPaths()
	if err != nil || paths == nil {
		logging.Warn("Could not resolve amux home for script trust registry: %v", err)
		return NewScriptTrust(".")
	}
	return NewScriptTrust(paths.Home)
}

// hashConfig returns the hex SHA-256 of the config content.
func hashConfig(configContent []byte) string {
	sum := sha256.Sum256(configContent)
	return hex.EncodeToString(sum[:])
}

// load reads the registry map. A missing file yields an empty map (no error);
// an unreadable or unparseable file yields an empty map and a logged warning, so
// callers fail closed.
func (t *ScriptTrust) load() map[string]string {
	if t == nil {
		return map[string]string{}
	}
	raw, err := os.ReadFile(t.path)
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		logging.Warn("Could not read script trust registry %s: %v", t.path, err)
		return map[string]string{}
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		logging.Warn("Ignoring corrupt script trust registry %s: %v", t.path, err)
		return map[string]string{}
	}
	if entries == nil {
		return map[string]string{}
	}
	return entries
}

// IsTrusted reports whether the user has approved the current content of
// repoPath's .amux/workspaces.json. It is fail-closed: a missing or corrupt
// registry, or any content change since approval, yields false.
func (t *ScriptTrust) IsTrusted(repoPath string, configContent []byte) bool {
	if t == nil {
		return false
	}
	key := data.NormalizePath(repoPath)
	if key == "" {
		return false
	}
	approved, ok := t.load()[key]
	if !ok {
		return false
	}
	return approved == hashConfig(configContent)
}

// Trust records configContent as the approved content for repoPath, writing the
// registry atomically (temp + fsync + rename) the same way the workspace store
// persists its JSON state.
func (t *ScriptTrust) Trust(repoPath string, configContent []byte) error {
	if t == nil {
		return nil
	}
	key := data.NormalizePath(repoPath)
	if key == "" {
		// Mirror IsTrusted's guard: an empty key can never be matched, so
		// recording one would "succeed" while granting no real trust.
		return nil
	}
	entries := t.load()
	entries[key] = hashConfig(configContent)

	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	return fsatomic.WriteFile(t.path, out, 0o644)
}
