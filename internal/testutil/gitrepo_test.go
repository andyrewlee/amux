package testutil

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInitRepoCreatesCommittedRepoOnBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := InitRepoWithBranch(t, "trunk")

	// RunGit returns trimmed output, so the branch name is directly comparable.
	if branch := RunGit(t, root, "rev-parse", "--abbrev-ref", "HEAD"); branch != "trunk" {
		t.Fatalf("HEAD branch = %q, want trunk", branch)
	}
	// Exactly one commit, and README is tracked in it.
	if count := RunGit(t, root, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("commit count = %q, want 1", count)
	}
	files := RunGit(t, root, "ls-files")
	if !strings.Contains(files, "README.md") {
		t.Fatalf("tracked files = %q, want README.md", files)
	}
	// The pinned identity is applied (no dependence on global git config).
	if author := RunGit(t, root, "log", "-1", "--format=%an"); author != "Test" {
		t.Fatalf("author = %q, want Test", author)
	}
}
