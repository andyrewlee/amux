package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveWorkspaceRejectsReusedPathDuringDeferredUnregisterRecovery(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/new-admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     "",
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected RemoveWorkspace() to reject reused live path before git lookup")
		return "", nil
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected RemoveWorkspace() to reject reused live path before unregister")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject reused live path during deferred unregister recovery")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestRemoveWorkspacePreservesDeferredUnregisterWhenCleanupPathIsAlreadyGone(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/missing-repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		return errWorkspaceCleanupRepoUnavailable
	}

	err := RemoveWorkspace("/tmp/missing-repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to report pending cleanup while unregister remains deferred")
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}

	state, marked, readErr := readWorkspaceCleanupState(workspacePath)
	if readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain while unregister is still pending")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending when staged cleanup path is already gone")
	}
	if state.CleanupPath != "" {
		t.Fatalf("cleanup path = %q, want empty", state.CleanupPath)
	}
}
