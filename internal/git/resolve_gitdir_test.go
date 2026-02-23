package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorktreeGitDir_NormalClone(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	gitDir, err := ResolveWorktreeGitDir(repo)
	if err != nil {
		t.Fatalf("ResolveWorktreeGitDir() error = %v", err)
	}

	want := filepath.Join(repo, ".git")
	if gitDir != want {
		t.Errorf("ResolveWorktreeGitDir() = %q, want %q", gitDir, want)
	}
}

func TestResolveWorktreeGitDir_Worktree(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	worktreePath := filepath.Join(t.TempDir(), "feature-wt")
	runGit(t, repo, "worktree", "add", "-b", "feature", worktreePath, "HEAD")

	gitDir, err := ResolveWorktreeGitDir(worktreePath)
	if err != nil {
		t.Fatalf("ResolveWorktreeGitDir() error = %v", err)
	}

	// Resolve symlinks for comparison (macOS: /var -> /private/var)
	want, _ := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	got, _ := filepath.EvalSymlinks(gitDir)
	if got != want {
		t.Errorf("ResolveWorktreeGitDir() = %q, want %q", got, want)
	}
}

func TestResolveWorktreeGitDir_NoGitFile(t *testing.T) {
	dir := t.TempDir()

	_, err := ResolveWorktreeGitDir(dir)
	if err == nil {
		t.Error("ResolveWorktreeGitDir() should return error for directory without .git")
	}
}

func TestResolveWorktreeGitDir_InvalidGitFile(t *testing.T) {
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("not a valid gitdir reference"), 0644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	_, err := ResolveWorktreeGitDir(dir)
	if err == nil {
		t.Error("ResolveWorktreeGitDir() should return error for invalid .git file")
	}
}
