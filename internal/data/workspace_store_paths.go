package data

import (
	"strings"
)

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
