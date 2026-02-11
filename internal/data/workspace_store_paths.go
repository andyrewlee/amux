package data

import (
	"path/filepath"
	"strings"
)

// canonicalLookupPath resolves a path to symlink-evaluated form for consistent
// comparison. Like NormalizePath, relative paths stay relative so that matching
// is independent of the process working directory.
func canonicalLookupPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if filepath.IsAbs(cleaned) {
		if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return filepath.Clean(cleaned)
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
	if existing.Name == "" && candidate.Name != "" {
		return true
	}
	return false
}
