package attempt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAttemptMetadataSaveLoad(t *testing.T) {
	root := t.TempDir()
	meta := NewMetadata()
	meta.IssueID = "issue-1"
	meta.IssueIdentifier = "ENG-1"
	meta.BranchName = "lin/ENG-1/abcd"
	if err := Save(root, meta); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.IssueID != "issue-1" {
		t.Fatalf("unexpected loaded metadata: %+v", loaded)
	}
	path := filepath.Join(root, ".amux", "attempt.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected attempt.json to exist: %v", err)
	}
}
