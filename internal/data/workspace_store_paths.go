package data

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

var repoFieldHintPattern = regexp.MustCompile(`"repo"\s*:\s*`)

func (s *WorkspaceStore) repoHintForWorkspaceID(id WorkspaceID) (string, bool) {
	data, err := os.ReadFile(s.workspacePath(id))
	if err != nil {
		return "", false
	}
	var ws workspaceJSON
	if err := json.Unmarshal(data, &ws); err == nil && strings.TrimSpace(ws.Repo) != "" {
		return ws.Repo, true
	}
	return repoHintFromRawJSON(data)
}

func repoHintFromRawJSON(data []byte) (string, bool) {
	offset := 0
	for {
		match := repoFieldHintPattern.FindIndex(data[offset:])
		if match == nil {
			return "", false
		}
		start := offset + match[1]
		value := strings.TrimLeft(string(data[start:]), " \t\r\n")
		if value == "" || value[0] != '"' {
			offset = start
			continue
		}
		repo, ok := parseJSONStringPrefix(value)
		if !ok {
			offset = start
			continue
		}
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return "", false
		}
		return repo, true
	}
}

func parseJSONStringPrefix(value string) (string, bool) {
	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
			if i >= len(value) {
				return "", false
			}
		case '"':
			var decoded string
			if err := json.Unmarshal([]byte(value[:i+1]), &decoded); err != nil {
				return "", false
			}
			return decoded, true
		}
	}
	return "", false
}

// canonicalLookupPath mirrors NormalizePath semantics for lookup comparisons:
// relative paths stay relative (CWD-independent), absolute paths are cleaned
// and symlink-resolved when possible.
func canonicalLookupPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	return NormalizePath(value)
}

func shouldPreferWorkspace(candidate, existing *Workspace) bool {
	if existing == nil {
		return true
	}
	if candidate == nil {
		return false
	}
	if existing.Archived && !candidate.Archived {
		return true
	}
	if !existing.Archived && candidate.Archived {
		return false
	}
	if existing.Created.IsZero() && !candidate.Created.IsZero() {
		return true
	}
	if !existing.Created.IsZero() && candidate.Created.IsZero() {
		return false
	}
	if candidate.Created.After(existing.Created) {
		return true
	}
	if existing.Created.After(candidate.Created) {
		return false
	}
	if existing.Name == "" && candidate.Name != "" {
		return true
	}
	if quality := workspaceMetadataQuality(candidate) - workspaceMetadataQuality(existing); quality != 0 {
		return quality > 0
	}
	return false
}

func workspaceMetadataQuality(ws *Workspace) int {
	if ws == nil {
		return 0
	}
	score := 0
	if strings.TrimSpace(ws.Name) != "" {
		score++
	}
	if strings.TrimSpace(ws.Branch) != "" {
		score++
	}
	if strings.TrimSpace(ws.Base) != "" {
		score++
	}
	if strings.TrimSpace(ws.Assistant) != "" {
		score++
	}
	if strings.TrimSpace(ws.ScriptMode) != "" {
		score++
	}
	if strings.TrimSpace(ws.Runtime) != "" {
		score++
	}
	if len(ws.Env) > 0 {
		score++
	}
	if len(ws.OpenTabs) > 0 {
		score++
	}
	return score
}
