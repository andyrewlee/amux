package app

import (
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestSessionWorkspacePathUsesPersistedWorktreeID(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	targetRoot := filepath.Join(t.TempDir(), "target")
	ws := &data.Workspace{Root: targetRoot}
	session := &sandboxSession{worktreeID: sandbox.ComputeWorktreeID(sourceRoot)}
	sb := sandbox.NewMockRemoteSandbox("sb-persisted-worktree")

	got := sessionWorkspacePath(sb, ws, session)
	want := sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{
		Cwd:        targetRoot,
		WorktreeID: session.worktreeID,
	})
	if got != want {
		t.Fatalf("sessionWorkspacePath() = %q, want %q", got, want)
	}
}
