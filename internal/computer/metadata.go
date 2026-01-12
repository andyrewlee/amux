package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

// ComputerMeta tracks the shared computer state for a provider.
// Note: There is ONE shared computer per provider, used by all projects.
type ComputerMeta struct {
	ComputerID string `json:"computerId"`
	CreatedAt  string `json:"createdAt"`
	ConfigHash string `json:"configHash"`
	Agent      Agent  `json:"agent"`
	Provider   string `json:"provider,omitempty"`
}

// ComputerStore stores computer metadata per provider.
// Stored globally at ~/.amux/computer.json
type ComputerStore struct {
	Computers map[string]ComputerMeta `json:"computers"`
	Provider  string                  `json:"provider,omitempty"`
}

// ComputeWorktreeID returns a stable ID based on the working directory path.
// This is used to isolate workspaces for different projects within the shared computer.
func ComputeWorktreeID(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	hash := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(hash[:])[:16]
}

// globalMetaPath returns the path to the global computer metadata file.
func globalMetaPath() (string, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.Home, "computer.json"), nil
}

// LoadComputerMeta loads the shared computer metadata for a provider.
// The cwd parameter is kept for API compatibility but is no longer used
// since metadata is now stored globally.
func LoadComputerMeta(_ string, provider string) (*ComputerMeta, error) {
	state, err := LoadComputerStore()
	if err != nil || state == nil {
		return nil, err
	}
	if provider == "" {
		provider = state.Provider
	}
	if provider == "" {
		return nil, nil
	}
	meta, ok := state.Computers[provider]
	if !ok {
		return nil, nil
	}
	return &meta, nil
}

// SaveComputerMeta saves the shared computer metadata for a provider.
// The cwd parameter is kept for API compatibility but is no longer used
// since metadata is now stored globally.
func SaveComputerMeta(_ string, provider string, meta ComputerMeta) error {
	state, err := LoadComputerStore()
	if err != nil {
		return err
	}
	if state == nil {
		state = &ComputerStore{Computers: map[string]ComputerMeta{}}
	}
	if state.Computers == nil {
		state.Computers = map[string]ComputerMeta{}
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
	state.Computers[provider] = meta
	if state.Provider == "" {
		state.Provider = provider
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// LoadComputerStore loads the global computer store from ~/.amux/computer.json
func LoadComputerStore() (*ComputerStore, error) {
	metaPath, err := globalMetaPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, nil
	}
	var state ComputerStore
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, nil
	}
	if state.Computers == nil {
		state.Computers = map[string]ComputerMeta{}
	}
	return &state, nil
}

// RemoveComputerMeta removes the shared computer metadata for a provider.
// The cwd parameter is kept for API compatibility but is no longer used
// since metadata is now stored globally.
func RemoveComputerMeta(_ string, provider string) error {
	state, err := LoadComputerStore()
	if err != nil || state == nil {
		return err
	}
	if provider == "" {
		provider = state.Provider
	}
	if provider == "" {
		return nil
	}
	delete(state.Computers, provider)
	metaPath, err := globalMetaPath()
	if err != nil {
		return err
	}
	if len(state.Computers) == 0 {
		return os.Remove(metaPath)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

func ComputeConfigHash(inputs map[string]any) string {
	payload := stableStringify(inputs)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])[:16]
}

func stableStringify(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		b, _ := json.Marshal(v)
		return string(b)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64, float32, int, int32, int64, uint, uint32, uint64:
		b, _ := json.Marshal(v)
		return string(b)
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, stableStringify(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, stableStringify(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, stableStringify(key)+":"+stableStringify(v[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
