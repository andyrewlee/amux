package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWorkspacePathForCreatePreservesUnregisterFromRetryMetadata(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	commonGitDir := filepath.Join(repoPath, ".git")
	adminDir := filepath.Join(commonGitDir, "worktrees", "feature")
	if err := os.MkdirAll(adminDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(adminDir) error = %v", err)
	}

	workspacePath := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(adminDir, "gitdir"), []byte(filepath.Join(workspacePath, ".git")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitdir) error = %v", err)
	}
	if _, err := ensureWorkspaceCleanupRetryMetadataWithContext(context.Background(), workspacePath, repoPath, true); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadataWithContext() error = %v", err)
	}

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		if gotRepoPath != repoPath {
			t.Fatalf("repo path = %q, want %q", gotRepoPath, repoPath)
		}
		if got, want := args[0]+" "+args[1], "rev-parse --git-common-dir"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return commonGitDir, nil
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(_ context.Context, gotRepoPath, gotWorkspacePath string) error {
		unregisterCalls++
		if gotRepoPath != repoPath {
			t.Fatalf("unregister repo path = %q, want %q", gotRepoPath, repoPath)
		}
		if gotWorkspacePath != workspacePath {
			t.Fatalf("unregister workspace path = %q, want %q", gotWorkspacePath, workspacePath)
		}
		return nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}

	if err := prepareWorkspacePathForCreate(repoPath, workspacePath); err != nil {
		t.Fatalf("prepareWorkspacePathForCreate() error = %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed during retry cleanup, err=%v", err)
	}
}

func TestPrepareWorkspacePathForCreateUsesRepoPathStoredInRetryMetadata(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	originalRepoPath := filepath.Join(t.TempDir(), "repo-a")
	currentRepoPath := filepath.Join(t.TempDir(), "repo-b")
	commonGitDir := filepath.Join(originalRepoPath, ".git")
	adminDir := filepath.Join(commonGitDir, "worktrees", "feature")
	if err := os.MkdirAll(adminDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(adminDir) error = %v", err)
	}

	workspacePath := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(adminDir, "gitdir"), []byte(filepath.Join(workspacePath, ".git")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitdir) error = %v", err)
	}
	if _, err := ensureWorkspaceCleanupRetryMetadataWithContext(context.Background(), workspacePath, originalRepoPath, true); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadataWithContext() error = %v", err)
	}

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		if gotRepoPath != originalRepoPath {
			t.Fatalf("repo path = %q, want %q", gotRepoPath, originalRepoPath)
		}
		if got, want := args[0]+" "+args[1], "rev-parse --git-common-dir"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return commonGitDir, nil
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(_ context.Context, gotRepoPath, gotWorkspacePath string) error {
		unregisterCalls++
		if gotRepoPath != originalRepoPath {
			t.Fatalf("unregister repo path = %q, want %q", gotRepoPath, originalRepoPath)
		}
		if gotWorkspacePath != workspacePath {
			t.Fatalf("unregister workspace path = %q, want %q", gotWorkspacePath, workspacePath)
		}
		return nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}

	if err := prepareWorkspacePathForCreate(currentRepoPath, workspacePath); err != nil {
		t.Fatalf("prepareWorkspacePathForCreate() error = %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed during retry cleanup, err=%v", err)
	}
}

func TestPrepareWorkspacePathForCreateRejectsReusedPathFromRetryMetadata(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	workspacePath := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(old.txt) error = %v", err)
	}
	if _, err := ensureWorkspaceCleanupRetryMetadataWithContext(context.Background(), workspacePath, repoPath, false); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadataWithContext() error = %v", err)
	}
	if err := os.Remove(filepath.Join(workspacePath, "old.txt")); err != nil {
		t.Fatalf("Remove(old.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile(new.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected create retry metadata reuse guard to fail before git commands")
		return "", nil
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected create retry metadata reuse guard to fail before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected create retry metadata reuse guard to fail before cleanup delete")
		return nil
	}

	err := prepareWorkspacePathForCreate(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected prepareWorkspacePathForCreate() to reject reused retry-metadata path")
	}
	if !strings.Contains(err.Error(), "pending cleanup") {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}
}

func TestPrepareWorkspacePathForCreateRecoversLegacyRetryMetadataWithoutFingerprint(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}
	if err := os.WriteFile(workspaceCleanupRetryMetadataPath(workspacePath), []byte("needs_unregister=false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(retryMetadata) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		t.Fatal("expected legacy retry cleanup to complete before git commands")
		return "", nil
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected legacy retry cleanup to complete without unregister")
		return nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}

	if err := prepareWorkspacePathForCreate("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("prepareWorkspacePathForCreate() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy retry workspace path to be removed during create prep, err=%v", err)
	}
}
