package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitRepositoryOps(t *testing.T) {
	repo := initRepo(t)

	if !IsGitRepository(repo) {
		t.Fatalf("IsGitRepository() should return true for repo")
	}

	nonRepo := t.TempDir()
	if IsGitRepository(nonRepo) {
		t.Fatalf("IsGitRepository() should return false for non-repo")
	}

	root, err := GetRepoRoot(repo)
	if err != nil {
		t.Fatalf("GetRepoRoot() error = %v", err)
	}
	rootEval := root
	if eval, err := filepath.EvalSymlinks(root); err == nil {
		rootEval = eval
	}
	repoEval := repo
	if eval, err := filepath.EvalSymlinks(repo); err == nil {
		repoEval = eval
	}
	if rootEval != repoEval {
		t.Fatalf("GetRepoRoot() = %s, want %s", rootEval, repoEval)
	}

	branch, err := GetCurrentBranch(repo)
	if err != nil {
		t.Fatalf("GetCurrentBranch() error = %v", err)
	}
	if branch != "main" {
		t.Fatalf("GetCurrentBranch() = %s, want main", branch)
	}

	runGit(t, repo, "remote", "add", "origin", "https://example.com/repo.git")
	remote, err := GetRemoteURL(repo, "origin")
	if err != nil {
		t.Fatalf("GetRemoteURL() error = %v", err)
	}
	if remote != "https://example.com/repo.git" {
		t.Fatalf("GetRemoteURL() = %s, want https://example.com/repo.git", remote)
	}

	// Ensure repo still clean after remote addition (no working tree changes).
	status, err := GetStatus(repo)
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if !status.Clean {
		t.Fatalf("expected clean status, got %+v", status)
	}

	// Add a temp file and ensure status is dirty.
	if err := os.WriteFile(filepath.Join(repo, "tmp.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}
	status, err = GetStatus(repo)
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.Clean {
		t.Fatalf("expected dirty status after tmp file")
	}
}
