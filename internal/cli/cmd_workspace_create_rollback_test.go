package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/andyrewlee/amux/internal/git"
)

func TestRollbackWorkspaceCreatePreservesExistingBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	branchName := "existing-feature"
	runGit(t, repoRoot, "branch", branchName)

	workspacePath := filepath.Join(t.TempDir(), "existing-feature")
	if err := gitpkg.CreateWorkspace(repoRoot, workspacePath, branchName, "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	if err := rollbackWorkspaceCreate(repoRoot, workspacePath, branchName, false); err != nil {
		t.Fatalf("rollbackWorkspaceCreate() error = %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, repoRoot, "branch", "--list", branchName)); got == "" {
		t.Fatalf("expected branch %q to remain after rollback", branchName)
	}
}

func TestRollbackWorkspaceCreateDeletesNewBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	branchName := "new-feature"
	workspacePath := filepath.Join(t.TempDir(), "new-feature")
	if err := gitpkg.CreateWorkspace(repoRoot, workspacePath, branchName, "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	if err := rollbackWorkspaceCreate(repoRoot, workspacePath, branchName, true); err != nil {
		t.Fatalf("rollbackWorkspaceCreate() error = %v", err)
	}

	if got := strings.TrimSpace(runGitOutput(t, repoRoot, "branch", "--list", branchName)); got != "" {
		t.Fatalf("expected branch %q to be deleted after rollback, got %q", branchName, got)
	}
}
