package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetStatusFast_CleanRepo(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	result, err := GetStatusFast(repo)
	if err != nil {
		t.Fatalf("GetStatusFast: %v", err)
	}
	if !result.Clean {
		t.Errorf("expected Clean=true for clean repo")
	}
	if result.TotalAdded != 0 || result.TotalDeleted != 0 {
		t.Errorf("expected zero line stats, got added=%d deleted=%d", result.TotalAdded, result.TotalDeleted)
	}
}

func TestGetStatusFast_DirtyRepo(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	// Create an untracked file
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := GetStatusFast(repo)
	if err != nil {
		t.Fatalf("GetStatusFast: %v", err)
	}
	if result.Clean {
		t.Errorf("expected Clean=false for dirty repo")
	}
	if len(result.Untracked) != 1 {
		t.Errorf("expected 1 untracked file, got %d", len(result.Untracked))
	}
	if result.TotalAdded != 0 || result.TotalDeleted != 0 {
		t.Errorf("expected zero line stats from fast mode, got added=%d deleted=%d", result.TotalAdded, result.TotalDeleted)
	}
}

func TestGetStatusFast_MatchesFull_ChangeList(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	// Create some changes: staged, unstaged, and untracked
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "staged.txt")

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fast, err := GetStatusFast(repo)
	if err != nil {
		t.Fatalf("GetStatusFast: %v", err)
	}
	full, err := GetStatus(repo)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}

	// Change lists must match
	if fast.Clean != full.Clean {
		t.Errorf("Clean mismatch: fast=%v full=%v", fast.Clean, full.Clean)
	}
	if len(fast.Staged) != len(full.Staged) {
		t.Errorf("Staged count mismatch: fast=%d full=%d", len(fast.Staged), len(full.Staged))
	}
	if len(fast.Unstaged) != len(full.Unstaged) {
		t.Errorf("Unstaged count mismatch: fast=%d full=%d", len(fast.Unstaged), len(full.Unstaged))
	}
	if len(fast.Untracked) != len(full.Untracked) {
		t.Errorf("Untracked count mismatch: fast=%d full=%d", len(fast.Untracked), len(full.Untracked))
	}

	// Verify fast mode has zero line stats while full has non-zero
	if fast.TotalAdded != 0 {
		t.Errorf("fast TotalAdded should be 0, got %d", fast.TotalAdded)
	}
	if full.TotalAdded == 0 {
		t.Errorf("full TotalAdded should be non-zero for dirty repo")
	}
}
