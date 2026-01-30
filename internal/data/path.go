package data

import "path/filepath"

// NormalizePath returns a cleaned path with symlinks resolved when possible.
// It avoids forcing absolute paths to preserve legacy IDs that may be relative.
func NormalizePath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return cleaned
	}
	return filepath.Clean(resolved)
}

func workspaceIdentity(repo, root string) string {
	return NormalizePath(repo) + "\n" + NormalizePath(root)
}

func legacyWorkspaceIdentity(repo, root string) string {
	return repo + root
}
