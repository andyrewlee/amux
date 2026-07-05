package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveWorkspaceRecoversMarkerWrittenBeforeRenameWithMatchingFingerprint(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/original-admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	fingerprint, err := workspaceCleanupRetryFingerprint(workspacePath)
	if err != nil {
		t.Fatalf("workspaceCleanupRetryFingerprint() error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:             "/tmp/repo",
		CleanupPath:          stagedPath,
		NeedsUnregister:      true,
		WorkspaceFingerprint: fingerprint,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if path != stagedPath {
			t.Fatalf("cleanup path = %q, want %q", path, stagedPath)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed, err=%v", err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged path to be removed, err=%v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to clear after recovery, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsRecreatedWorktreeEvenWhenGitRefLooksTheSame(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	gitPath := filepath.Join(workspacePath, ".git")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	gitContent := []byte("gitdir: /tmp/original-admin\n")
	if err := os.WriteFile(gitPath, gitContent, 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(old.txt) error = %v", err)
	}
	gitInfo, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("Stat(.git) error = %v", err)
	}
	fingerprint, err := workspaceCleanupRetryFingerprint(workspacePath)
	if err != nil {
		t.Fatalf("workspaceCleanupRetryFingerprint() error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:               "/tmp/repo",
		CleanupPath:            stagedPath,
		NeedsUnregister:        false,
		WorkspaceGitRef:        strings.TrimSpace(string(gitContent)),
		WorkspaceGitRefModTime: gitInfo.ModTime().UnixNano(),
		WorkspaceFingerprint:   fingerprint,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	if err := os.Remove(filepath.Join(workspacePath, "old.txt")); err != nil {
		t.Fatalf("Remove(old.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "replacement.txt"), []byte("replacement"), 0o644); err != nil {
		t.Fatalf("WriteFile(replacement.txt) error = %v", err)
	}
	if err := os.WriteFile(gitPath, gitContent, 0o644); err != nil {
		t.Fatalf("WriteFile(.git rewrite) error = %v", err)
	}
	modTime := gitInfo.ModTime()
	if err := os.Chtimes(gitPath, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(.git) error = %v", err)
	}

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected recreated worktree to be rejected before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected recreated worktree to be rejected before cleanup delete")
		return nil
	}

	err = RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject recreated worktree with matching .git metadata")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestWorkspaceCleanupRetryFingerprintRejectsEscapingGitSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	outsideGit := filepath.Join(tmpDir, "outside-git")
	if err := os.WriteFile(outsideGit, []byte("gitdir: /tmp/outside-admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outsideGit) error = %v", err)
	}
	if err := os.Symlink(outsideGit, filepath.Join(workspacePath, ".git")); err != nil {
		t.Fatalf("Symlink(.git) error = %v", err)
	}

	fingerprint, err := workspaceCleanupRetryFingerprint(workspacePath)
	if err == nil {
		t.Fatal("workspaceCleanupRetryFingerprint() expected escaping .git symlink error, got nil")
	}
	if fingerprint != "" {
		t.Fatalf("workspaceCleanupRetryFingerprint() fingerprint = %q, want empty on error", fingerprint)
	}
}

func TestRemoveWorkspaceRejectsNoFingerprintMarkerWhenRetryMetadataFingerprintMismatches(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}
	if err := ensureWorkspaceCleanupRetryMetadata(workspacePath, "/tmp/repo", false); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadata() error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:    "/tmp/repo",
		CleanupPath: stagedPath,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	if err := os.Remove(filepath.Join(workspacePath, "keep.txt")); err != nil {
		t.Fatalf("Remove(keep.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "replacement.txt"), []byte("replacement"), 0o644); err != nil {
		t.Fatalf("WriteFile(replacement.txt) error = %v", err)
	}

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected mismatched marker-only recovery to be rejected before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected mismatched marker-only recovery to be rejected before cleanup delete")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject mismatched marker-only recovery")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected live workspace path to remain on disk, err=%v", err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged cleanup path to remain absent, err=%v", err)
	}
}

func TestRemoveWorkspaceTimeoutCancelsRetryFingerprintWithinRecoveryBudget(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origWorktreeTimeout := worktreeTimeout
	origWorktreeRecoveryReserve := worktreeRecoveryReserve
	origWorkspaceCleanupRetryFingerprintCtx := workspaceCleanupRetryFingerprintCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		worktreeTimeout = origWorktreeTimeout
		worktreeRecoveryReserve = origWorktreeRecoveryReserve
		workspaceCleanupRetryFingerprintCtx = origWorkspaceCleanupRetryFingerprintCtx
	}()

	worktreeTimeout = 40 * time.Millisecond
	worktreeRecoveryReserve = 20 * time.Millisecond

	workspacePath := filepath.Join(t.TempDir(), "timeout-fingerprint")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			return "worktree " + workspacePath, nil
		case "worktree remove " + workspacePath + " --force":
			<-ctx.Done()
			return "", errors.Join(context.DeadlineExceeded, ctx.Err())
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected recovery to stop before staged cleanup delete when fingerprinting times out")
		return nil
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected recovery to stop before unregister when fingerprinting times out")
		return nil
	}

	fingerprintCalls := 0
	workspaceCleanupRetryFingerprintCtx = func(ctx context.Context, path string) (string, error) {
		fingerprintCalls++
		if path != workspacePath {
			t.Fatalf("workspacePath = %q, want %q", path, workspacePath)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("expected retry fingerprint context to have a deadline")
		}
		<-ctx.Done()
		return "", ctx.Err()
	}

	start := time.Now()
	err := RemoveWorkspace("/tmp/repo", workspacePath)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when retry fingerprinting exceeds the recovery deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if fingerprintCalls != 1 {
		t.Fatalf("fingerprint calls = %d, want 1", fingerprintCalls)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected recovery to honor its deadline, elapsed=%v", elapsed)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected workspace path to remain in place after fingerprint timeout, err=%v", err)
	}
	if _, err := os.Stat(workspaceCleanupRetryMetadataPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected retry metadata write to be skipped after fingerprint timeout, err=%v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to remain absent after fingerprint timeout, err=%v", err)
	}
}
