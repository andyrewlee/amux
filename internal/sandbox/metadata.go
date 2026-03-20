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
	SandboxID     string   `json:"sandboxId"`
	CreatedAt     string   `json:"createdAt"`
	Agent         Agent    `json:"agent"`
	Provider      string   `json:"provider,omitempty"`
	WorktreeID    string   `json:"worktreeId,omitempty"`
	WorkspaceRoot string   `json:"workspaceRoot,omitempty"`
	Project       string   `json:"project,omitempty"`
	NeedsSyncDown *bool    `json:"needsSyncDown,omitempty"`
	WorkspaceIDs  []string `json:"workspaceIds,omitempty"`
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
	store, err := LoadSandboxStore()
	if err != nil || store == nil {
		return nil, err
	}
	key := findSandboxMetaKey(store, cwd, provider)
	if key == "" {
		return nil, nil
	}
	meta := store.Sandboxes[key]
	return sandboxMetaCopy(meta, key), nil
}

func sandboxMetaCopy(meta SandboxMeta, storedWorktreeID string) *SandboxMeta {
	metaCopy := meta
	if strings.TrimSpace(metaCopy.WorktreeID) == "" {
		metaCopy.WorktreeID = storedWorktreeID
	}
	return &metaCopy
}

// findSandboxMetaKey returns the store key for sandbox metadata matching cwd
// and provider. Returns "" if no match is found or the match is ambiguous.
func findSandboxMetaKey(store *SandboxStore, cwd, provider string) string {
	worktreeID := ComputeWorktreeID(cwd)
	if meta, ok := store.Sandboxes[worktreeID]; ok {
		if provider == "" || meta.Provider == provider {
			return worktreeID
		}
	}

	canonicalRoot := canonicalSandboxMetaPath(cwd)
	if canonicalRoot == "" {
		return ""
	}

	var matchKey string
	var legacyKey string
	for key, candidate := range store.Sandboxes {
		if provider != "" && candidate.Provider != "" && candidate.Provider != provider {
			continue
		}
		candidateRoot := canonicalSandboxMetaPath(candidate.WorkspaceRoot)
		if candidateRoot != canonicalRoot && !(candidateRoot == "" && strings.TrimSpace(candidate.WorktreeID) == worktreeID) {
			continue
		}

		if provider == "" {
			if matchKey != "" {
				return "" // ambiguous
			}
			matchKey = key
			continue
		}
		if candidate.Provider == provider {
			if matchKey != "" {
				return "" // ambiguous
			}
			matchKey = key
			continue
		}
		if candidate.Provider == "" && legacyKey == "" {
			legacyKey = key
		}
	}
	if matchKey != "" {
		return matchKey
	}
	return legacyKey
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
	meta.CreatedAt = strings.TrimSpace(meta.CreatedAt)
	if meta.CreatedAt == "" {
		meta.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	meta.Provider = provider
	workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = strings.TrimSpace(cwd)
	}
	meta.WorkspaceRoot = canonicalSandboxMetaPath(workspaceRoot)
	if meta.WorkspaceRoot == "" {
		meta.WorkspaceRoot = workspaceRoot
	}
	worktreeID := ComputeWorktreeID(cwd)
	if strings.TrimSpace(meta.WorktreeID) == "" {
		meta.WorktreeID = worktreeID
	}
	for key, existing := range state.Sandboxes {
		if key == worktreeID {
			continue
		}
		if canonicalSandboxMetaPath(existing.WorkspaceRoot) != meta.WorkspaceRoot {
			continue
		}
		if provider != "" && existing.Provider != "" && existing.Provider != provider {
			continue
		}
		delete(state.Sandboxes, key)
	}
	state.Sandboxes[worktreeID] = meta
	return writeStoreToFile(state)
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
		return &SandboxStore{Sandboxes: map[string]SandboxMeta{}}, nil
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
	delete(state.Sandboxes, ComputeWorktreeID(cwd))
	return writeStoreToFile(state)
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
	deleted := false
	for key, meta := range state.Sandboxes {
		if meta.SandboxID == id {
			delete(state.Sandboxes, key)
			deleted = true
		}
	}
	if !deleted {
		return nil
	}
	return writeStoreToFile(state)
}

// writeStoreToFile marshals the store and writes it to the global metadata file.
// If the store has no sandboxes, the file is removed instead.
func writeStoreToFile(store *SandboxStore) error {
	metaPath, err := globalMetaPath()
	if err != nil {
		return err
	}
	if len(store.Sandboxes) == 0 {
		return os.Remove(metaPath)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return writeSandboxStoreFile(metaPath, data)
}

func writeSandboxStoreFile(metaPath string, data []byte) error {
	metaDir := filepath.Dir(metaPath)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(metaDir, "sandbox.json.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return err
	}
	success = true
	return nil
}

// updateSandboxMeta loads the store, finds metadata for cwd/provider, applies
// the mutation in-place, and writes the store back in a single load+write cycle.
func updateSandboxMeta(cwd, provider string, mutate func(meta *SandboxMeta)) error {
	store, err := LoadSandboxStore()
	if err != nil || store == nil {
		return err
	}
	key := findSandboxMetaKey(store, cwd, provider)
	if key == "" {
		return nil
	}
	meta := store.Sandboxes[key]
	if strings.TrimSpace(meta.SandboxID) == "" {
		return nil
	}
	mutate(&meta)
	store.Sandboxes[key] = meta
	return writeStoreToFile(store)
}

func SetSandboxMetaNeedsSync(cwd, provider string, needsSync bool) error {
	return updateSandboxMeta(cwd, provider, func(meta *SandboxMeta) {
		meta.NeedsSyncDown = boolPtr(needsSync)
	})
}

func MetaNeedsSync(meta *SandboxMeta, defaultValue bool) bool {
	if meta == nil || meta.NeedsSyncDown == nil {
		return defaultValue
	}
	return *meta.NeedsSyncDown
}

func SetSandboxMetaWorkspaceIDs(cwd, provider string, workspaceIDs []string) error {
	return updateSandboxMeta(cwd, provider, func(meta *SandboxMeta) {
		meta.WorkspaceIDs = normalizeWorkspaceIDs(workspaceIDs)
	})
}

func MoveSandboxMeta(oldCwd, newCwd, provider string) error {
	oldRoot := strings.TrimSpace(oldCwd)
	newRoot := strings.TrimSpace(newCwd)
	if oldRoot == "" || newRoot == "" || oldRoot == newRoot {
		return nil
	}

	store, err := LoadSandboxStore()
	if err != nil || store == nil {
		return err
	}

	oldKey := findSandboxMetaKey(store, oldRoot, provider)
	if oldKey == "" {
		return nil
	}
	meta := store.Sandboxes[oldKey]
	if strings.TrimSpace(meta.SandboxID) == "" {
		return nil
	}
	if strings.TrimSpace(provider) == "" {
		provider = meta.Provider
	}

	newWorktreeID := ComputeWorktreeID(newRoot)
	meta.Provider = provider
	meta.WorkspaceRoot = canonicalSandboxMetaPath(newRoot)
	if meta.WorkspaceRoot == "" {
		meta.WorkspaceRoot = newRoot
	}
	if strings.TrimSpace(meta.WorktreeID) == "" {
		meta.WorktreeID = newWorktreeID
	}

	// Delete old key if different from new.
	if oldKey != newWorktreeID {
		delete(store.Sandboxes, oldKey)
	}

	// Clean up other entries with matching canonical root.
	for key, existing := range store.Sandboxes {
		if key == newWorktreeID {
			continue
		}
		if canonicalSandboxMetaPath(existing.WorkspaceRoot) != meta.WorkspaceRoot {
			continue
		}
		if provider != "" && existing.Provider != "" && existing.Provider != provider {
			continue
		}
		delete(store.Sandboxes, key)
	}

	store.Sandboxes[newWorktreeID] = meta
	return writeStoreToFile(store)
}

func canonicalSandboxMetaPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

func normalizeWorkspaceIDs(workspaceIDs []string) []string {
	if len(workspaceIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(workspaceIDs))
	normalized := make([]string, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func boolPtr(v bool) *bool {
	return &v
}
