package sidebar

import (
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

// canonicalWorkspacePath compares workspace repo paths across storage forms
// (a stored relative path vs a live absolute one). It delegates to
// data.CanonicalPath — the match contract (absolutize + resolve-or-fallback),
// deliberately not data.NormalizePath, whose relative preservation exists for
// identity hashing and would make a rel-vs-abs pair compare unequal here.
func canonicalWorkspacePath(path string) string {
	return data.CanonicalPath(path)
}
