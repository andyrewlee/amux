package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveWorkspaceRetriesUnregisteredLeftoverAfterMarkerWriteFailure(t *testing.T) {
	origRunGitCtx := runGitCtx
	origWriteRetryMarkerFile := writeRetryMarkerFile
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		writeRetryMarkerFile = origWriteRetryMarkerFile
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "stale-ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	worktreeListCalls := 0
	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			worktreeListCalls++
			if worktreeListCalls == 1 {
				return "worktree " + workspacePath, nil
			}
			return "", nil
		case "worktree remove " + workspacePath + " --force":
			return "", errors.New("leftover cleanup required")
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}

	writeRetryMarkerFile = func(string, []byte, os.FileMode) error {
		return errors.New("marker write failed")
	}
	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when marker write fails")
	}
	if !strings.Contains(err.Error(), "marker write failed") {
		t.Fatalf("expected marker write failure, got %v", err)
	}
	if _, err := os.Stat(workspaceCleanupRetryMetadataPath(workspacePath)); err != nil {
		t.Fatalf("expected internal retry metadata to remain, err=%v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected external cleanup marker to be absent after failed write, err=%v", err)
	}

	writeRetryMarkerFile = origWriteRetryMarkerFile
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("retry RemoveWorkspace() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed on retry, err=%v", err)
	}
	if _, err := os.Stat(prunedWorkspaceRetryMarkerPath(workspacePath)); !os.IsNotExist(err) {
		t.Fatalf("expected external cleanup marker to clear after retry, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerlessManagedStaleWorkspace(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := filepath.Join(t.TempDir(), "repo")
	workspacePath := filepath.Join(home, ".amux", "workspaces", filepath.Base(repoPath), "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		t.Fatal("expected markerless stale workspace to be returned to caller for scoped cleanup")
		return nil
	}

	err := RemoveWorkspace(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to return recoverable unregistered workspace error")
	}
	if !IsUnregisteredWorkspacePathError(err) {
		t.Fatalf("expected ErrUnregisteredWorkspacePath, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected managed stale workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceRetriesTimeoutCleanupAfterMarkerWriteFailure(t *testing.T) {
	origRunGitCtx := runGitCtx
	origWriteRetryMarkerFile := writeRetryMarkerFile
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	origUnregisterWorktreeCtx := unregisterWorktreeCtx
	defer func() {
		runGitCtx = origRunGitCtx
		writeRetryMarkerFile = origWriteRetryMarkerFile
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
		unregisterWorktreeCtx = origUnregisterWorktreeCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "timeout-stale-ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	gitFile := filepath.Join(workspacePath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /tmp/admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	worktreeListCalls := 0
	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			worktreeListCalls++
			if worktreeListCalls == 1 {
				return "worktree " + workspacePath, nil
			}
			return "", nil
		case "worktree remove " + workspacePath + " --force":
			if err := os.Remove(gitFile); err != nil && !os.IsNotExist(err) {
				t.Fatalf("Remove(.git) error = %v", err)
			}
			return "", context.DeadlineExceeded
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}

	writeRetryMarkerFile = func(string, []byte, os.FileMode) error {
		return errors.New("marker write failed")
	}
	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when timeout marker write fails")
	}
	if !strings.Contains(err.Error(), "marker write failed") {
		t.Fatalf("expected marker write failure, got %v", err)
	}
	if _, err := os.Stat(workspaceCleanupRetryMetadataPath(workspacePath)); err != nil {
		t.Fatalf("expected internal retry metadata to remain after timeout marker write failure, err=%v", err)
	}

	writeRetryMarkerFile = origWriteRetryMarkerFile
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}
	unregisterWorktreeCtx = func(context.Context, string, string) error {
		return errWorkspaceCleanupRepoUnavailable
	}

	err = RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected retry RemoveWorkspace() to report pending cleanup while unregister remains deferred")
	}
	if !IsWorkspaceCleanupPendingError(err) {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed on timeout retry, err=%v", err)
	}
	state, marked, err := readWorkspaceCleanupState(workspacePath)
	if err != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", err)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain after deferred unregister retry")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after deferred timeout retry")
	}
	if state.CleanupPath != "" {
		t.Fatalf("cleanup path = %q, want empty", state.CleanupPath)
	}
}

func TestRemoveWorkspaceRejectsReusedPathFromRetryMetadata(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "stale-ws")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(old.txt) error = %v", err)
	}
	if err := ensureWorkspaceCleanupRetryMetadata(workspacePath, "/tmp/repo", false); err != nil {
		t.Fatalf("ensureWorkspaceCleanupRetryMetadata() error = %v", err)
	}
	if err := os.Remove(filepath.Join(workspacePath, "old.txt")); err != nil {
		t.Fatalf("Remove(old.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile(new.txt) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected retry metadata reuse guard to fail before cleanup delete")
		return nil
	}

	err := RemoveWorkspace("/tmp/repo", workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject reused retry-metadata path")
	}
	if !strings.Contains(err.Error(), "pending cleanup remains") {
		t.Fatalf("expected pending cleanup error, got %v", err)
	}
}

func TestRemoveWorkspaceRecoversLegacyRetryMetadataWithoutFingerprint(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	workspacePath := filepath.Join(t.TempDir(), "legacy-retry-ws")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "nested", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(file.txt) error = %v", err)
	}
	if err := os.WriteFile(workspaceCleanupRetryMetadataPath(workspacePath), []byte("pending workspace cleanup\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(retryMetadata) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy retry workspace path to be removed, err=%v", err)
	}
}
