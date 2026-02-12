package sidebar

import (
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func sameWorkspaceByCanonicalPaths(left, right *data.Workspace) bool {
	if left == nil || right == nil {
		return false
	}
	if left.ID() == right.ID() {
		return true
	}

	leftRoot := canonicalWorkspacePath(left.Root)
	rightRoot := canonicalWorkspacePath(right.Root)
	if leftRoot == "" || rightRoot == "" || leftRoot != rightRoot {
		return false
	}

	leftRepo := canonicalWorkspacePath(left.Repo)
	rightRepo := canonicalWorkspacePath(right.Repo)
	if leftRepo == "" || rightRepo == "" {
		return true
	}
	return leftRepo == rightRepo
}

func canonicalWorkspacePath(path string) string {
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
