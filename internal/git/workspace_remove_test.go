package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveWorkspaceMissingPathIsIdempotent(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "already-gone")
	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", nil
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
}

func TestRemoveWorkspaceRejectsUnregisteredExistingDirectory(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "stale-ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject leftover unregistered workspace directory")
	}
	if !strings.Contains(err.Error(), "still exists on disk") {
		t.Fatalf("expected leftover directory error, got %v", err)
	}
	if _, statErr := os.Stat(workspacePath); statErr != nil {
		t.Fatalf("expected unregistered workspace path to remain on disk, err=%v", statErr)
	}
}

func TestRemoveWorkspaceRejectsUnsafeUnregisteredExistingDirectory(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return "", nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected unregistered path to be left alone before recursive delete")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", "/")
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject unsafe leftover unregistered path")
	}
	if !strings.Contains(err.Error(), "still exists on disk") {
		t.Fatalf("expected leftover directory error, got %v", err)
	}
}

func TestRemoveWorkspaceTimeoutPersistsHiddenCleanupAndRetriesDirectly(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "timeout-hidden-cleanup")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/pruned"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			return "worktree " + workspacePath, nil
		case "worktree remove " + workspacePath + " --force":
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		return nil
	}

	var stagedPath string
	removeCalls := 0
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		removeCalls++
		if !strings.Contains(filepath.Base(path), ".timeout-hidden-cleanup.amux-prune-") {
			t.Fatalf("cleanup path = %q, want hidden staged path", path)
		}
		stagedPath = path
		return errors.New("cleanup failed")
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when hidden cleanup fails")
	}
	if !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("expected cleanup failure in error, got %v", err)
	}
	if unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", unregisterCalls)
	}
	if removeCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", removeCalls)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to remain absent, err=%v", err)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("expected hidden staged path to remain, err=%v", err)
	}
	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup state to remain after failed hidden cleanup")
	}
	if state.RepoPath != "/tmp/repo" {
		t.Fatalf("repo path = %q, want %q", state.RepoPath, "/tmp/repo")
	}
	if state.CleanupPath != stagedPath {
		t.Fatalf("cleanup path = %q, want %q", state.CleanupPath, stagedPath)
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending until hidden cleanup finishes")
	}

	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if path != stagedPath {
			t.Fatalf("retry cleanup path = %q, want %q", path, stagedPath)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("retry RemoveWorkspace() error = %v", err)
	}
	if unregisterCalls != 2 {
		t.Fatalf("expected retry to rerun unregister until cleanup clears, got %d calls", unregisterCalls)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to be cleared, err=%v", err)
	}
}

func TestRemoveWorkspaceTimeoutPersistsNeedsUnregisterUntilRetrySucceeds(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "timeout-unregister-retry")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/pruned"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			return "worktree " + workspacePath, nil
		case "worktree remove " + workspacePath + " --force":
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}

	unregisterCalls := 0
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		unregisterCalls++
		if unregisterCalls == 1 {
			return errors.New("unregister failed")
		}
		return nil
	}

	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected cleanup to wait until unregister succeeds")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when unregister fails")
	}
	if !strings.Contains(err.Error(), "unregister failed") {
		t.Fatalf("expected unregister failure in error, got %v", err)
	}

	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup state after unregister failure")
	}
	if state.RepoPath != "/tmp/repo" {
		t.Fatalf("repo path = %q, want %q", state.RepoPath, "/tmp/repo")
	}
	if state.CleanupPath == "" {
		t.Fatal("expected hidden cleanup path after unregister failure")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected cleanup state to keep unregister pending after failure")
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to remain absent, err=%v", err)
	}

	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if path != state.CleanupPath {
			t.Fatalf("retry cleanup path = %q, want %q", path, state.CleanupPath)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("retry RemoveWorkspace() error = %v", err)
	}
	if unregisterCalls != 2 {
		t.Fatalf("unregister calls = %d, want 2", unregisterCalls)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to clear after retry, err=%v", err)
	}
}

func TestRemoveWorkspaceRetryUsesCurrentRepoPathWhenItOwnsAdminDir(t *testing.T) {
	origRunGitCtx := runGitCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	repoAPath := filepath.Join(t.TempDir(), "repo-a")
	repoBPath := filepath.Join(t.TempDir(), "repo-b")
	commonDirB := filepath.Join(repoBPath, ".git")
	adminDirB := filepath.Join(commonDirB, "worktrees", "pending-cleanup")
	if err := os.MkdirAll(adminDirB, 0o755); err != nil {
		t.Fatalf("MkdirAll(adminDirB) error = %v", err)
	}

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(stagedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stagedPath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        repoAPath,
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(adminDirB, "gitdir"), []byte(filepath.Join(workspacePath, ".git")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitdir) error = %v", err)
	}

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		if strings.Join(args, " ") != "rev-parse --git-common-dir" {
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
		}
		switch gotRepoPath {
		case repoBPath:
			return commonDirB, nil
		case repoAPath:
			return "", fmt.Errorf("%w: %s", errWorkspaceCleanupRepoUnavailable, gotRepoPath)
		default:
			t.Fatalf("unexpected repo path %q", gotRepoPath)
			return "", nil
		}
	}

	unregisterWorktreeCtx = func(_ context.Context, repoPath, gotWorkspacePath string) error {
		if repoPath != repoBPath {
			t.Fatalf("unregister repo path = %q, want %q", repoPath, repoBPath)
		}
		if gotWorkspacePath != workspacePath {
			t.Fatalf("workspace path = %q, want %q", gotWorkspacePath, workspacePath)
		}
		return nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		if path != stagedPath {
			t.Fatalf("cleanup path = %q, want %q", path, stagedPath)
		}
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace(repoBPath, workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup marker to clear after retry, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsLegacyUnregisterMarkerWithoutRepoPath(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	markerPath := prunedWorkspaceRetryMarkerPath(workspacePath)
	if err := os.WriteFile(markerPath, []byte("u:/tmp/staged\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected repo-less legacy marker to fail before unregister")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo-b", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject repo-less legacy unregister marker")
	}
	if !strings.Contains(err.Error(), "missing repo path") {
		t.Fatalf("expected missing repo path error, got %v", err)
	}
}

func TestRemoveWorkspaceRejectsLegacyPendingCleanupMarkerWhenPathExists(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "pending-cleanup")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(prunedWorkspaceRetryMarkerPath(workspacePath), []byte("pruned workspace cleanup pending\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(markerPath) error = %v", err)
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected legacy pending cleanup marker to fail before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected legacy pending cleanup marker to preserve the live workspace path")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject legacy pending cleanup marker with live path")
	}
	if !strings.Contains(err.Error(), "legacy pending cleanup marker") {
		t.Fatalf("expected legacy cleanup error, got %v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerWrittenBeforeRenameEvenIfLivePathIsRegistered(t *testing.T) {
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		t.Fatal("expected same-path registered workspace to be rejected before unregister")
		return nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected same-path registered workspace to be rejected before cleanup delete")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject same-path registered workspace during pending cleanup")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerWrittenBeforeRenameWhenLivePathNotRegistered(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	workspaceRoot := t.TempDir()
	workspacePath := filepath.Join(workspaceRoot, "pending-cleanup")
	stagedPath := filepath.Join(workspaceRoot, ".pending-cleanup.amux-prune-1")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		RepoPath:        "/tmp/repo",
		CleanupPath:     stagedPath,
		NeedsUnregister: false,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}
	runGitCtx = func(ctx context.Context, repoPath string, args ...string) (string, error) {
		if repoPath != "/tmp/repo" {
			t.Fatalf("repo path = %q, want %q", repoPath, "/tmp/repo")
		}
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject unregistered live path for missing cleanup target")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup conflict, got %v", err)
	}
}
