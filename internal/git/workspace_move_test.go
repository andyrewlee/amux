package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveWorktreeInvokesGitMove(t *testing.T) {
	stubOps(t)

	called := false
	ops.runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		called = true
		if got, want := strings.Join(args, " "), "worktree move -- /ws/repo/feature /ws/repo-new/feature"; got != want {
			t.Fatalf("worktree move args = %q, want %q", got, want)
		}
		return "", nil
	}

	if err := MoveWorktree("/tmp/repo", "/ws/repo/feature", "/ws/repo-new/feature"); err != nil {
		t.Fatalf("MoveWorktree() error = %v", err)
	}
	if !called {
		t.Fatalf("expected ops.runGitCtx to be called for the move")
	}
}

func TestMoveWorktreeRefusesExistingDestination(t *testing.T) {
	stubOps(t)

	called := false
	ops.runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		called = true
		return "", nil
	}

	dest := t.TempDir()
	if err := MoveWorktree("/tmp/repo", "/ws/repo/feature", dest); err == nil {
		t.Fatalf("expected existing destination to be refused")
	}
	if called {
		t.Fatalf("expected no git invocation when destination exists")
	}
}

func TestMoveWorktreeReportsDestinationStatError(t *testing.T) {
	stubOps(t)

	ops.runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		t.Fatalf("expected no git invocation when destination is not statable")
		return "", nil
	}

	// A destination whose parent is a regular file makes os.Stat fail with
	// an error other than NotExist, which must surface rather than pass git
	// a bogus destination.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	dest := filepath.Join(blocker, "nested")

	if err := MoveWorktree("/tmp/repo", "/ws/repo/feature", dest); err == nil {
		t.Fatalf("expected stat error to surface as a move refusal")
	}
}
