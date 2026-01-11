package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const workspaceMetaPath = ".amux/workspace.json"

// WorkspaceMeta tracks a workspace's sandbox state.
type WorkspaceMeta struct {
	WorkspaceID string `json:"workspaceId"`
	SandboxID   string `json:"sandboxId"`
	CreatedAt   string `json:"createdAt"`
	ConfigHash  string `json:"configHash"`
	Agent       Agent  `json:"agent"`
}

func ComputeWorkspaceID(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	hash := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(hash[:])[:16]
}

func LoadWorkspaceMeta(cwd string) (*WorkspaceMeta, error) {
	path := filepath.Join(cwd, workspaceMetaPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var meta WorkspaceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func SaveWorkspaceMeta(cwd string, meta WorkspaceMeta) error {
	metaDir := filepath.Join(cwd, ".amux")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	meta.CreatedAt = strings.TrimSpace(meta.CreatedAt)
	if meta.CreatedAt == "" {
		meta.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cwd, workspaceMetaPath), data, 0o644)
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
