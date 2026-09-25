package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestSameWorkspaceByCanonicalPaths_Contract pins the IDENTITY-question
// semantics: ID fast path, then canonical root AND canonical repo. The repo
// leg distinguishes a workspace re-created at the same path under a
// different repo — the case where the app's path-only
// rootsReferToSameWorkspace would say same (correctly, for routing) and this
// predicate must not (correctly, for rebinding).
func TestSameWorkspaceByCanonicalPaths_Contract(t *testing.T) {
	repo := t.TempDir()
	otherRepo := t.TempDir()
	root := t.TempDir()

	base := data.NewWorkspace("ws", "br", "main", repo, root)

	for _, tt := range []struct {
		name  string
		left  *data.Workspace
		right *data.Workspace
		want  bool
	}{
		{"same identity", base, data.NewWorkspace("ws", "br", "main", repo, root), true},
		{"nil left", nil, base, false},
		{"nil right", base, nil, false},
		{"different root", base, data.NewWorkspace("ws", "br", "main", repo, t.TempDir()), false},
		{"same root different repo is NOT same", base, data.NewWorkspace("ws", "br", "main", otherRepo, root), false},
	} {
		if got := sameWorkspaceByCanonicalPaths(tt.left, tt.right); got != tt.want {
			t.Errorf("%s: sameWorkspaceByCanonicalPaths = %v, want %v", tt.name, got, tt.want)
		}
	}
}
