package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
)

func TestRemoveWorkspaceRejectsMarkerlessManagedAliasStaleWorkspace(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo-real-name")
	workspacePath := filepath.Join(t.TempDir(), ".amux", "workspaces", "project-name-drift", "feature")
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
		t.Fatal("expected alias stale workspace to be returned to caller for scoped cleanup")
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
		t.Fatalf("expected managed alias workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsGitBearingManagedStaleWorkspace(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	repoPath := filepath.Join(t.TempDir(), "repo-real-name")
	workspacePath := filepath.Join(t.TempDir(), ".amux", "workspaces", "repo-real-name", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: /tmp/admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.git) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected .git-bearing managed stale workspace to be rejected before cleanup delete")
		return nil
	}

	err := RemoveWorkspace(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject .git-bearing managed stale workspace")
	}
	if !strings.Contains(err.Error(), "has a .git file but is not a registered worktree") {
		t.Fatalf("expected unmanaged .git error, got %v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerlessManagedWorkspaceUnderSymlinkRootAlias(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)
	realRoot := filepath.Join(t.TempDir(), "real-workspaces")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}
	amuxDir := filepath.Join(home, ".amux")
	if err := os.MkdirAll(amuxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(amuxDir) error = %v", err)
	}
	if err := os.Symlink(realRoot, filepath.Join(amuxDir, "workspaces")); err != nil {
		t.Fatalf("Symlink(workspaces root) error = %v", err)
	}

	repoPath := filepath.Join(t.TempDir(), "repo-real-name")
	workspacePath := filepath.Join(realRoot, "project-name-drift", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		t.Fatal("expected symlink-root stale workspace to be returned to caller for scoped cleanup")
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
		t.Fatalf("expected managed symlink-root workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerlessManagedWorkspaceUnderConfiguredSymlinkRoot(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real-workspaces")
	configuredRoot := filepath.Join(tmp, "workspaces-link")
	if err := os.Symlink(realRoot, configuredRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv(config.WorkspacesRootEnvVar, configuredRoot)

	repoPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(realRoot, "repo", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		t.Fatal("expected symlink-root stale workspace to be returned to caller for scoped cleanup")
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
		t.Fatalf("expected configured symlink-root workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceRejectsMarkerlessManagedWorkspaceUnderConfiguredRootEnv(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	customRoot := filepath.Join(t.TempDir(), "custom-workspaces")
	t.Setenv(config.WorkspacesRootEnvVar, customRoot)

	repoPath := filepath.Join(t.TempDir(), "repo-real-name")
	workspacePath := filepath.Join(customRoot, "repo-real-name", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", nil
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		t.Fatal("expected configured-root stale workspace to be returned to caller for scoped cleanup")
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
		t.Fatalf("expected configured-root workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceLookupFailureDoesNotCleanupDifferentRepoUnderManagedRoot(t *testing.T) {
	origRunGitCtx := runGitCtx
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		runGitCtx = origRunGitCtx
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	customRoot := filepath.Join(t.TempDir(), "custom-workspaces")
	t.Setenv(config.WorkspacesRootEnvVar, customRoot)

	repoPath := filepath.Join(t.TempDir(), "repo-a")
	workspacePath := filepath.Join(customRoot, "repo-b", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		if got, want := strings.Join(args, " "), "worktree list --porcelain"; got != want {
			t.Fatalf("git args = %q, want %q", got, want)
		}
		return "", context.DeadlineExceeded
	}
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		t.Fatal("expected different-repo stale workspace to be returned to caller for scoped cleanup")
		return nil
	}

	err := RemoveWorkspace(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to reject managed path for a different repo")
	}
	if !IsUnregisteredWorkspacePathError(err) {
		t.Fatalf("expected ErrUnregisteredWorkspacePath, got %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected different-repo workspace path to remain for caller cleanup, err=%v", err)
	}
}

func TestRemoveWorkspaceResumesPendingCleanupAcrossWorkspaceRootAliases(t *testing.T) {
	origRemoveWorkspacePathCtx := removeWorkspacePathCtx
	defer func() {
		removeWorkspacePathCtx = origRemoveWorkspacePathCtx
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)

	realRoot := filepath.Join(t.TempDir(), "real-workspaces")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}
	amuxDir := filepath.Join(home, ".amux")
	if err := os.MkdirAll(amuxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(amuxDir) error = %v", err)
	}
	aliasRoot := filepath.Join(amuxDir, "workspaces")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatalf("Symlink(aliasRoot) error = %v", err)
	}

	workspacePath := filepath.Join(aliasRoot, "repo", "feature")
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspace parent) error = %v", err)
	}
	stagedPath := filepath.Join(realRoot, "repo", ".feature.amux-prune-1")
	if err := os.MkdirAll(stagedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stagedPath) error = %v", err)
	}
	if err := writeWorkspaceCleanupState(workspacePath, workspaceCleanupState{
		CleanupPath: stagedPath,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState() error = %v", err)
	}

	var removedPath string
	removeWorkspacePathCtx = func(_ context.Context, path string) error {
		removedPath = path
		return os.RemoveAll(path)
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if removedPath != stagedPath {
		t.Fatalf("removed cleanup path = %q, want %q", removedPath, stagedPath)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged cleanup path to be removed, err=%v", err)
	}
	if marked, err := hasPendingWorkspaceCleanup(workspacePath); err != nil {
		t.Fatalf("hasPendingWorkspaceCleanup() error = %v", err)
	} else if marked {
		t.Fatal("expected pending cleanup marker to clear after alias resume")
	}
}

func TestRemoveWorkspacePreservesPendingUnregisterWhenAdminDirLookupMisses(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(commonGitDir, "worktrees", "other"), 0o755); err != nil {
		t.Fatalf("MkdirAll(worktrees) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(commonGitDir, "worktrees", "other", "gitdir"), []byte("/tmp/other/.git\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(gitdir) error = %v", err)
	}

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

	runGitCtx = func(_ context.Context, gotRepoPath string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --git-common-dir":
			if gotRepoPath != repoPath {
				t.Fatalf("repo path = %q, want %q", gotRepoPath, repoPath)
			}
			return commonGitDir, nil
		case "worktree list --porcelain":
			if gotRepoPath != repoPath {
				t.Fatalf("repo path = %q, want %q", gotRepoPath, repoPath)
			}
			return "worktree " + workspacePath, nil
		default:
			t.Fatalf("unexpected git args %q", strings.Join(args, " "))
			return "", nil
		}
	}
	unregisterWorktreeCtx = unregisterWorktreeAdminDirWithContext
	removeWorkspacePathCtx = func(context.Context, string) error {
		t.Fatal("expected staged cleanup delete to be skipped when unregister lookup misses")
		return nil
	}

	err := RemoveWorkspace(repoPath, workspacePath)
	if err == nil {
		t.Fatal("expected RemoveWorkspace() to fail when unregister admin-dir lookup misses")
	}
	if !strings.Contains(err.Error(), "admin dir not found") {
		t.Fatalf("expected admin-dir lookup miss, got %v", err)
	}
	state, marked, readErr := readWorkspaceCleanupState(workspacePath)
	if readErr != nil {
		t.Fatalf("readWorkspaceCleanupState() error = %v", readErr)
	}
	if !marked {
		t.Fatal("expected cleanup marker to remain after unregister lookup miss")
	}
	if !state.NeedsUnregister {
		t.Fatal("expected unregister to remain pending after lookup miss")
	}
	if state.CleanupPath != stagedPath {
		t.Fatalf("cleanup path = %q, want %q", state.CleanupPath, stagedPath)
	}
}
