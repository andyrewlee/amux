package git

import (
	"os"
	"path/filepath"
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
