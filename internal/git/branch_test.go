package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAheadBehind(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	runGit(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(repo+"/feature.txt", []byte("feature"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature commit")

	ahead, behind, err := AheadBehind(repo, "main", "feature")
	if err != nil {
		t.Fatalf("AheadBehind error: %v", err)
	}
	if ahead != 1 || behind != 0 {
		t.Fatalf("expected ahead=1 behind=0, got ahead=%d behind=%d", ahead, behind)
	}

	runGit(t, repo, "checkout", "main")
	if err := os.WriteFile(repo+"/main.txt", []byte("main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, repo, "add", "main.txt")
	runGit(t, repo, "commit", "-m", "main commit")

	ahead, behind, err = AheadBehind(repo, "main", "feature")
	if err != nil {
		t.Fatalf("AheadBehind error: %v", err)
	}
	if ahead != 1 || behind != 1 {
		t.Fatalf("expected ahead=1 behind=1, got ahead=%d behind=%d", ahead, behind)
	}
}

func TestRebaseInProgress(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	if RebaseInProgress(repo) {
		t.Fatalf("expected no rebase in progress")
	}
	path, err := RunGit(repo, "rev-parse", "--git-path", "rebase-merge")
	if err != nil {
		t.Fatalf("rev-parse git-path failed: %v", err)
	}
	// git rev-parse --git-path can return a relative path; resolve against repo
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !RebaseInProgress(repo) {
		t.Fatalf("expected rebase in progress")
	}
}
