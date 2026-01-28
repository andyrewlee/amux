package linear

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheSaveLoadIssues(t *testing.T) {
	tmp := t.TempDir()
	cache := NewCache(tmp)
	issues := []Issue{{ID: "1", Identifier: "ENG-1", Title: "Test", UpdatedAt: time.Now(), Account: "work"}}
	if err := cache.SaveIssues("work", "viewer-1", issues); err != nil {
		t.Fatalf("SaveIssues: %v", err)
	}
	loaded, err := cache.LoadIssues("work", "viewer-1")
	if err != nil {
		t.Fatalf("LoadIssues: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "1" {
		t.Fatalf("unexpected loaded issues: %+v", loaded)
	}

	// Ensure file path exists
	path := filepath.Join(tmp, "linear", "work", "viewer-1", "issues.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file to exist: %v", err)
	}
}
