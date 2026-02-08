package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAndRemoveWorkspace(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	workspacePath := filepath.Join(t.TempDir(), "feature-wt")

	if err := CreateWorkspace(repo, workspacePath, "feature-wt", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected workspace path to exist: %v", err)
	}

	// Verify .git file exists in worktree
	gitPath := filepath.Join(workspacePath, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		t.Fatalf("expected .git to exist in workspace: %v", err)
	}

	if err := RemoveWorkspace(repo, workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed, err=%v", err)
	}
}

func TestRemoveWorkspaceWithUntrackedFiles(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	workspacePath := filepath.Join(t.TempDir(), "untracked-wt")

	if err := CreateWorkspace(repo, workspacePath, "untracked-wt", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	// Add untracked files that would cause "Directory not empty" error
	untrackedDir := filepath.Join(workspacePath, "node_modules", "some-package")
	if err := os.MkdirAll(untrackedDir, 0o755); err != nil {
		t.Fatalf("failed to create untracked dir: %v", err)
	}
	untrackedFile := filepath.Join(untrackedDir, "index.js")
	if err := os.WriteFile(untrackedFile, []byte("module.exports = {}"), 0o644); err != nil {
		t.Fatalf("failed to create untracked file: %v", err)
	}

	// RemoveWorkspace should succeed despite untracked files
	if err := RemoveWorkspace(repo, workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}

	// Directory should be fully removed
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected workspace path to be removed, err=%v", err)
	}
}

func TestRemoveWorkspaceRefusesUnmanagedFallbackCleanup(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	roguePath := filepath.Join(t.TempDir(), "not-a-worktree")
	if err := os.MkdirAll(roguePath, 0o755); err != nil {
		t.Fatalf("mkdir rogue path: %v", err)
	}
	keepFile := filepath.Join(roguePath, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write rogue file: %v", err)
	}

	err := RemoveWorkspace(repo, roguePath)
	if err == nil {
		t.Fatalf("expected RemoveWorkspace() to reject unmanaged path")
	}
	if !strings.Contains(err.Error(), "refusing fallback cleanup") {
		t.Fatalf("expected safety error, got %v", err)
	}
	if _, statErr := os.Stat(keepFile); statErr != nil {
		t.Fatalf("expected unmanaged path to remain untouched: %v", statErr)
	}
}

func TestRemoveWorkspaceAlreadyRemovedExternallyIsIdempotent(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	workspacePath := filepath.Join(t.TempDir(), "externally-removed-wt")
	if err := CreateWorkspace(repo, workspacePath, "externally-removed-wt", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	// Simulate external deletion before app-side cleanup.
	runGit(t, repo, "worktree", "remove", workspacePath, "--force")
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected external removal to delete worktree path, err=%v", err)
	}

	// App-side deletion should be idempotent and succeed.
	if err := RemoveWorkspace(repo, workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() should succeed for already-removed worktree: %v", err)
	}
}
