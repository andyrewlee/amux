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

func TestCreateWorkspaceReusesExistingBranch(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	originalPath := filepath.Join(t.TempDir(), "existing-branch-a")
	if err := CreateWorkspace(repo, originalPath, "existing-branch", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace(original) error = %v", err)
	}
	if err := RemoveWorkspace(repo, originalPath); err != nil {
		t.Fatalf("RemoveWorkspace(original) error = %v", err)
	}

	reusedPath := filepath.Join(t.TempDir(), "existing-branch-b")
	if err := CreateWorkspace(repo, reusedPath, "existing-branch", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace(reused) error = %v", err)
	}
	defer func() { _ = RemoveWorkspace(repo, reusedPath) }()

	branch, err := RunGit(reusedPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("RunGit(reusedPath) error = %v", err)
	}
	if branch != "existing-branch" {
		t.Fatalf("branch = %q, want %q", branch, "existing-branch")
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

	// Create a directory with a .git file that is NOT a registered worktree.
	unmanaged := filepath.Join(t.TempDir(), "unmanaged-ws")
	if err := os.MkdirAll(unmanaged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmanaged, ".git"), []byte("gitdir: /nonexistent"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RemoveWorkspace(repo, unmanaged)
	if err == nil {
		t.Fatal("expected error for unmanaged worktree with .git file")
	}
	if !strings.Contains(err.Error(), "not a registered worktree") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveWorkspaceAlreadyRemovedExternallyIsIdempotent(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	// Path that does not exist and is not a registered worktree.
	nonexistent := filepath.Join(t.TempDir(), "already-gone")

	err := RemoveWorkspace(repo, nonexistent)
	if err != nil {
		t.Fatalf("expected idempotent nil, got: %v", err)
	}
}
