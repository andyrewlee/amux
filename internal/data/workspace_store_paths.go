package data

import (
	"path/filepath"
	"strings"
)

func canonicalLookupPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	cleaned := filepath.Clean(value)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	return filepath.Clean(cleaned)
}
