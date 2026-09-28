package sidebar

import (
	"github.com/andyrewlee/amux/internal/data"
)

// sameWorkspaceByCanonicalPaths answers the IDENTITY question "is this the
// same workspace record?" for sidebar rebinding: ID fast-path, then
// canonical root AND canonical repo. The repo leg is the discriminator for
// a workspace re-created at the same path under a different repo — same
// root, but not the same workspace.
//
// Deliberately stricter than app's rootsReferToSameWorkspace, which answers
// the PATH question for message routing (a git-status result stamped with a
// root belongs to the workspace at that path, whatever its repo) and has no
// repo to compare in any case.
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
