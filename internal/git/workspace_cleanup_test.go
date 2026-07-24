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

func TestWorktreeAdminDirForWorkspaceResolvesRelativeGitdirPaths(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	commonGitDir := filepath.Join(repoPath, ".git")
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	adminDir := filepath.Join(commonGitDir, "worktrees", "feature")
	gitdirFile := filepath.Join(adminDir, "gitdir")

	if err := os.MkdirAll(adminDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(adminDir) error = %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	relativeGitdir, err := filepath.Rel(adminDir, filepath.Join(workspacePath, ".git"))
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}
	if err := os.WriteFile(gitdirFile, []byte(relativeGitdir+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitdirFile) error = %v", err)
	}

	runGitCtx = func(ctx context.Context, dir string, args ...string) (string, error) {
		if dir != repoPath {
			t.Fatalf("repo path = %q, want %q", dir, repoPath)
		}
		if got, want := strings.Join(args, " "), "rev-parse --git-common-dir"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return commonGitDir, nil
	}

	gotCommonGitDir, gotAdminDir, err := worktreeAdminDirForWorkspace(context.Background(), repoPath, workspacePath)
	if err != nil {
		t.Fatalf("worktreeAdminDirForWorkspace() error = %v", err)
	}
	if gotCommonGitDir != commonGitDir {
		t.Fatalf("common git dir = %q, want %q", gotCommonGitDir, commonGitDir)
	}
	if gotAdminDir != adminDir {
		t.Fatalf("admin dir = %q, want %q", gotAdminDir, adminDir)
	}
}

func TestRemoveWorkspaceRejectsMissingGitDirWhenWorktreeListFailsAndPathRemains(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "orphaned-workspace")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", context.DeadlineExceeded
	}

	err := RemoveWorkspace("/tmp/missing-repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject orphaned path that still exists on disk")
	}
	if !strings.Contains(err.Error(), "still exists on disk") {
		t.Fatalf("expected leftover directory error, got %v", err)
	}
	if _, statErr := os.Stat(workspacePath); statErr != nil {
		t.Fatalf("expected orphaned workspace path to remain on disk, err=%v", statErr)
	}
}

func TestRemoveWorkspaceRecoversNoFingerprintMarkerWhenRetryMetadataRemains(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo")
	commonGitDir := filepath.Join(repoPath, ".git")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoPath) error = %v", err)
	}

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}
	if _, err := ensureWorkspaceCleanupRetryMetadataWithContext(context.Background(), workspacePath, repoPath, true); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadataWithContext() error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        repoPath,
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		if gotRepoPath != repoPath {
			t.Fatalf("repo path = %q, want %q", gotRepoPath, repoPath)
		}
		if got, want := strings.Join(args, " "), "rev-parse --git-common-dir"; got != want {
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
		if path != stagedPath {
			t.Fatalf("cleanup path = %q, want %q", path, stagedPath)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace(repoPath, workspacePath); err != nil {
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

// TestRemoveWorkspaceFinishesCleanupWhenUnrelatedStrayReappears covers the
// real-world deadlock: after a workspace was staged for cleanup, a background
// process (a package manager, a file watcher) recreates an unrelated stray at
// the original path (e.g. `<ws>/apps/...`). The stray is not the recoverable
// workspace — no git fingerprint match — so it must NOT block the delete.
// The staged worktree (the thing actually marked for deletion) is removed and
// the marker cleared, while the stray content is left untouched. Previously
// this deadlocked forever with "pending cleanup remains".
func TestRemoveWorkspaceFinishesCleanupWhenUnrelatedStrayReappears(t *testing.T) {
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
	strayFile := filepath.Join(workspacePath, "replacement.txt")
	if err := os.WriteFile(strayFile, []byte("replacement"), 0o644); err != nil {
		t.Fatalf("WriteFile(replacement.txt) error = %v", err)
	}
	if err := os.MkdirAll(stagedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stagedPath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	// Real capability shape: repo present, worktree admin already gone, so
	// unregister is a clean no-op and cleanup can finalize.
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		return nil
	}
	var removedPaths []string
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		removedPaths = append(removedPaths, path)
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("expected cleanup to finish past an unrelated stray, got %v", err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged path removed, err=%v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker cleared, err=%v", err)
	}
	// The stray content must be preserved — cleanup only removes the staged
	// worktree, never whatever reappeared at the original path.
	if _, err := os.Stat(strayFile); err != nil {
		t.Fatalf("expected stray content preserved, err=%v", err)
	}
	for _, p := range removedPaths {
		if p == workspacePath {
			t.Fatal("cleanup must not remove the reappeared workspace path itself")
		}
	}
}

func TestRemoveWorkspaceDefersUnregisterWhenOriginalRepoIsGone(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "missing-repo")
	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(stagedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stagedPath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        repoPath,
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return errWorkspaceCleanupRepoUnavailable
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if path != stagedPath {
			t.Fatalf("cleanup path = %q, want %q", path, stagedPath)
		}
		return os.RemoveAll(path)
	}

	err := RemoveWorkspace("/tmp/other-repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to report pending cleanup while unregister remains deferred")
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged path to be removed, err=%v", err)
	}
	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain after deferred unregister")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after repo-unavailable cleanup")
	}
	if state.CleanupPath != "" {
		t.Fatalf("cleanup path = %q, want empty", state.CleanupPath)
	}

	err = RemoveWorkspace("/tmp/other-repo", workspacePath)
	if err == nil {
		t.Fatal("expected retry RemoveWorkspace() to keep reporting pending cleanup while repo is unavailable")
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected pending cleanup error on retry, got %v", err)
	}
	state, marked, err = readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() after retry error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain while repo is still unavailable")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after repeated repo-unavailable retry")
	}

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return nil
	}
	if err := RemoveWorkspace("/tmp/other-repo", workspacePath); err != nil {
		t.Fatalf("final retry RemoveWorkspace() error = %v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to clear once unregister succeeds, err=%v", err)
	}
}

func TestRemoveWorkspaceClearsMarkerWhenCleanupPathIsAlreadyGone(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return nil
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to clear when staged path is already gone, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerCleanupPathOutsideWorkspaceParent(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	outsideCleanupPath := filepath.Join(t.TempDir(), "outside-cleanup")
	if err := os.MkdirAll(outsideCleanupPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(outsideCleanupPath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     outsideCleanupPath,
		NeedsUnregister: false,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected unexpected cleanup path to be rejected before delete")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject cleanup path outside workspace parent")
	}
	if !strings.Contains(err.Error(), "unexpected pruned workspace cleanup path") {
		t.Fatalf("expected unexpected cleanup path error, got %v", err)
	}
}

func TestRemoveWorkspaceTimeoutUsesFreshRecoveryTimeout(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origWorktreeTimeout := worktreeTimeout
	origWorktreeRecoveryReserve := worktreeRecoveryReserve
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		worktreeTimeout = origWorktreeTimeout
		worktreeRecoveryReserve = origWorktreeRecoveryReserve
	}()

	worktreeTimeout = 100 * time.Millisecond
	worktreeRecoveryReserve = 10 * time.Millisecond

	workspacePath := filepath.Join(t.TempDir(), "timeout-fresh-recovery")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/pruned"), 0o644); err != nil {
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

	unregisterWorktreeCtx = func(context.Context, string, string) error {
		return nil
	}
	removeWorkspacePathCtx = func(ctx context.Context, path string) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected recovery cleanup context to have a deadline")
		}
		if remaining := time.Until(deadline); remaining < 50*time.Millisecond {
			t.Fatalf("expected fresh recovery budget, remaining=%v", remaining)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
}
