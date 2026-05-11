package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveWorkspaceTimeoutReturnsPendingErrorWhenUnregisterRetryIsDeferred(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "timeout-deferred-unregister")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/pruned"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		if gotRepoPath != "/tmp/repo" {
			t.Fatalf("repo path = %q, want %q", gotRepoPath, "/tmp/repo")
		}
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			return "worktree " + workspacePath, nil
		case "worktree remove " + workspacePath + " --force":
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		case "rev-parse --git-common-dir":
			return "", errWorkspaceCleanupRepoUnavailable
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return errWorkspaceCleanupRepoUnavailable
	}

	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if !strings.Contains(filepath.Base(path), ".amux-prune-") {
			t.Fatalf("unexpected cleanup path %q", path)
		}
		return os.RemoveAll(path)
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to report pending cleanup while unregister is deferred")
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}

	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain after deferred unregister")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after deferred timeout cleanup")
	}
	if state.CleanupPath != "" {
		t.Fatalf("cleanup path = %q, want empty", state.CleanupPath)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to remain absent, err=%v", err)
	}
}
