package data

import "path/filepath"

// NormalizePath returns a cleaned path with symlinks resolved when possible.
func NormalizePath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return cleaned
	}
	return filepath.Clean(resolved)
}

func workspaceIdentity(repo, root string) string {
	return NormalizePath(repo) + "\n" + NormalizePath(root)
}
