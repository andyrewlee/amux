package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

// SandboxMeta tracks the most recent sandbox for a worktree.
type SandboxMeta struct {
	SandboxID  string `json:"sandboxId"`
	CreatedAt  string `json:"createdAt"`
	Agent      Agent  `json:"agent"`
	Provider   string `json:"provider,omitempty"`
	WorktreeID string `json:"worktreeId,omitempty"`
	Project    string `json:"project,omitempty"`
}

// SandboxStore stores sandbox metadata per worktree.
// Stored globally at ~/.amux/sandbox.json
// Keys are worktree IDs.
type SandboxStore struct {
	Sandboxes map[string]SandboxMeta `json:"sandboxes"`
}

// ComputeWorktreeID returns a stable ID based on the working directory path.
// This is used to isolate workspaces for different projects within a sandbox.
func ComputeWorktreeID(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	hash := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(hash[:])[:16]
}

// globalMetaPath returns the path to the global sandbox metadata file.
func globalMetaPath() (string, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.Home, "sandbox.json"), nil
}

// LoadSandboxMeta loads sandbox metadata for the current worktree.
func LoadSandboxMeta(cwd, provider string) (*SandboxMeta, error) {
	state, err := LoadSandboxStore()
	if err != nil || state == nil {
		return nil, err
	}
	worktreeID := ComputeWorktreeID(cwd)
	meta, ok := state.Sandboxes[worktreeID]
	if !ok {
		return nil, nil
	}
	if provider != "" && meta.Provider != "" && meta.Provider != provider {
		return nil, nil
	}
	return &meta, nil
}

// SaveSandboxMeta saves sandbox metadata for the current worktree.
func SaveSandboxMeta(cwd, provider string, meta SandboxMeta) error {
	state, err := LoadSandboxStore()
	if err != nil {
		return err
	}
	if state == nil {
		state = &SandboxStore{Sandboxes: map[string]SandboxMeta{}}
	}
	if state.Sandboxes == nil {
		state.Sandboxes = map[string]SandboxMeta{}
	}
	metaPath, err := globalMetaPath()
	if err != nil {
		return err
	}
	metaDir := filepath.Dir(metaPath)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	meta.CreatedAt = strings.TrimSpace(meta.CreatedAt)
	if meta.CreatedAt == "" {
		meta.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	meta.Provider = provider
	meta.WorktreeID = ComputeWorktreeID(cwd)
	state.Sandboxes[meta.WorktreeID] = meta
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// LoadSandboxStore loads the global sandbox store from ~/.amux/sandbox.json
func LoadSandboxStore() (*SandboxStore, error) {
	metaPath, err := globalMetaPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil
	}
	var state SandboxStore
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, nil
	}
	if state.Sandboxes == nil {
		state.Sandboxes = map[string]SandboxMeta{}
	}
	return &state, nil
}

// RemoveSandboxMeta removes sandbox metadata for the current worktree.
func RemoveSandboxMeta(cwd, _ string) error {
	state, err := LoadSandboxStore()
	if err != nil || state == nil {
		return err
	}
	worktreeID := ComputeWorktreeID(cwd)
	delete(state.Sandboxes, worktreeID)
	metaPath, err := globalMetaPath()
	if err != nil {
		return err
	}
	if len(state.Sandboxes) == 0 {
		return os.Remove(metaPath)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// RemoveSandboxMetaByID removes sandbox metadata that matches the given sandbox ID.
func RemoveSandboxMetaByID(id string) error {
	state, err := LoadSandboxStore()
	if err != nil || state == nil {
		return err
	}
	if id == "" {
		return nil
	}
	for key, meta := range state.Sandboxes {
		if meta.SandboxID == id {
			delete(state.Sandboxes, key)
		}
	}
	metaPath, err := globalMetaPath()
	if err != nil {
		return err
	}
	if len(state.Sandboxes) == 0 {
		return os.Remove(metaPath)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}
