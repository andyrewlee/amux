package git

import (
	"path/filepath"
	"testing"
)

// TestGetUntrackedFileContentStartFailure: a launch failure (here a
// nonexistent repository directory) must surface as a displayable
// DiffResult.Error — never as a successful empty diff. The outer error
// stays nil: collection failures travel inside the result by design.
func TestGetUntrackedFileContentStartFailure(t *testing.T) {
	skipIfNoGit(t)

	missingRepo := filepath.Join(t.TempDir(), "missing")
	res, err := GetUntrackedFileContent(missingRepo, "untracked.txt")
	if err != nil {
		t.Fatalf("outer error must stay nil for collection failures, got %v", err)
	}
	if res == nil {
		t.Fatal("expected a DiffResult carrying the failure")
	}
	if res.Path != "untracked.txt" {
		t.Fatalf("res.Path = %q, want untracked.txt", res.Path)
	}
	if res.Error == "" {
		t.Fatal("res.Error must describe the launch failure — a start failure is not an empty diff")
	}
	if res.Empty {
		t.Fatal("res.Empty must be false: a failed collection is not 'no changes'")
	}
}
