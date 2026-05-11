package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
)

func TestRemoveWorkspaceLookupFailureMissingGitPreservesUnregisterRecoveryForManagedPath(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacesRoot := filepath.Join(t.TempDir(), "workspaces")
	t.Setenv(config.WorkspacesRootEnvVar, workspacesRoot)

	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", context.DeadlineExceeded
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		return errors.New("unregister failed")
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected cleanup delete to wait for unregister recovery")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when unregister recovery fails")
	}
	if !strings.Contains(err.Error(), "unregister failed") {
		t.Fatalf("expected unregister failure, got %v", err)
	}

	state, marked, readErr := readWorkspaceCleanupState(workspacePath)
	if readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain after unregister recovery failure")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after lookup recovery failure")
	}
	if state.CleanupPath == "" {
		t.Fatal("expected staged cleanup path after lookup recovery failure")
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be staged away, err=%v", err)
	}
	if _, err := os.Stat(state.CleanupPath); err != nil {
		t.Fatalf("expected staged cleanup path to remain, err=%v", err)
	}
}

func TestRemoveWorkspaceLookupFailureMissingGitRejectsUnrelatedManagedAncestorOutsideConfiguredRoot(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacesRoot := filepath.Join(t.TempDir(), "configured-workspaces")
	t.Setenv(config.WorkspacesRootEnvVar, workspacesRoot)

	workspacePath := filepath.Join(t.TempDir(), ".amux", "workspaces", "repo", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", context.DeadlineExceeded
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected unrelated managed-ancestor path to be returned to caller")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject unrelated stale workspace path")
	}
	if !IsUnregisteredWorkspacePathError(err) {
		t.Fatalf("expected ErrUnregisteredWorkspacePath, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected unrelated workspace path to remain on disk, err=%v", err)
	}
	if _, marked, readErr := readWorkspaceCleanupState(workspacePath); readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	} else if marked {
		t.Fatal("expected no cleanup marker for unrelated workspace path")
	}
}

func TestRemoveWorkspaceLookupFailureMissingGitRejectsDefaultRootWhenCustomRootConfigured(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.WorkspacesRootEnvVar, filepath.Join(t.TempDir(), "configured-workspaces"))

	workspacePath := filepath.Join(home, ".amux", "workspaces", "repo", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", context.DeadlineExceeded
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected default-root path to be returned to caller when a custom root is configured")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject default-root stale path outside configured root")
	}
	if !IsUnregisteredWorkspacePathError(err) {
		t.Fatalf("expected ErrUnregisteredWorkspacePath, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected default-root workspace path to remain on disk, err=%v", err)
	}
	if _, marked, readErr := readWorkspaceCleanupState(workspacePath); readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	} else if marked {
		t.Fatal("expected no cleanup marker for default-root path outside configured root")
	}
}

func TestRemoveWorkspaceRejectsMarkerlessWorkspaceOutsideManagedRoots(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	repoPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(tmp, "real-workspaces", "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected unrelated markerless workspace path to be returned to caller")
		return nil
	}

	err := RemoveWorkspace(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject unrelated markerless workspace path")
	}
	if !IsUnregisteredWorkspacePathError(err) {
		t.Fatalf("expected ErrUnregisteredWorkspacePath, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected unrelated workspace path to remain on disk, err=%v", err)
	}
	if _, marked, readErr := readWorkspaceCleanupState(workspacePath); readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	} else if marked {
		t.Fatal("expected no cleanup marker for unrelated markerless workspace path")
	}
}
