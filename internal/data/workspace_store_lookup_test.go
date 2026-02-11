package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStore_LoadMetadataForFallsBackToPathMatch(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	// Create real directories for repo and workspace root so that
	// canonicalLookupPath can resolve them.
	base := t.TempDir()
	repoDir := filepath.Join(base, "myrepo")
	rootDir := filepath.Join(repoDir, ".amux", "workspaces", "feature")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}

	// Simulate a workspace stored under a stale ID (e.g., from before a
	// symlink change caused the hash to differ). We write directly into the
	// store with an arbitrary ID that won't match the current hash.
	staleID := WorkspaceID("stale-id-before-rename")
	staleDir := filepath.Join(storeRoot, string(staleID))
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(staleDir) error = %v", err)
	}
	staleJSON := `{
		"name": "feature",
		"branch": "feature",
		"repo": "` + repoDir + `",
		"root": "` + rootDir + `",
		"created": "2024-06-01T00:00:00Z",
		"assistant": "codex",
		"script_mode": "concurrent",
		"env": {"KEY": "value"}
	}`
	if err := os.WriteFile(filepath.Join(staleDir, "workspace.json"), []byte(staleJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(stale) error = %v", err)
	}

	// Plant a corrupt entry in the store so the fallback scan exercises the
	// "skip unreadable" path.
	corruptID := WorkspaceID("corrupt-entry-abc123")
	corruptDir := filepath.Join(storeRoot, string(corruptID))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "workspace.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	// Build a discovered workspace using the same real paths.
	// The ID() hash won't match staleID, so the fast path misses.
	discovered := &Workspace{
		Name:   "feature",
		Branch: "feature",
		Repo:   repoDir,
		Root:   rootDir,
	}

	// Verify the ID-based lookup would miss (staleID != computed ID).
	if discovered.ID() == staleID {
		t.Fatal("expected stale ID to differ from computed ID")
	}

	// LoadMetadataFor should fall back to the path-scan and find the match.
	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found stored metadata via fallback")
	}

	// Verify stored metadata was merged.
	if discovered.Assistant != "codex" {
		t.Errorf("Assistant = %v, want 'codex'", discovered.Assistant)
	}
	if discovered.ScriptMode != "concurrent" {
		t.Errorf("ScriptMode = %v, want 'concurrent'", discovered.ScriptMode)
	}
	if discovered.Env["KEY"] != "value" {
		t.Errorf("Env[KEY] = %v, want 'value'", discovered.Env["KEY"])
	}
	expectedTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if !discovered.Created.Equal(expectedTime) {
		t.Errorf("Created = %v, want %v", discovered.Created, expectedTime)
	}

	// Discovery fields should be preserved.
	if discovered.Branch != "feature" {
		t.Errorf("Branch = %v, want 'feature'", discovered.Branch)
	}
}

func TestWorkspaceStore_FallbackPrefersActiveOverArchived(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	base := t.TempDir()
	repoDir := filepath.Join(base, "myrepo")
	rootDir := filepath.Join(repoDir, ".amux", "workspaces", "feature")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}

	// Write two stale entries for the same canonical workspace: one archived,
	// one active. Directory listing order is alphabetical, so "aaa" sorts
	// before "zzz". We make the archived entry sort first to verify the
	// fallback doesn't just return the first match.
	archivedID := WorkspaceID("aaa-archived-stale")
	archivedDir := filepath.Join(storeRoot, string(archivedID))
	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(archivedDir) error = %v", err)
	}
	archivedJSON := `{
		"name": "feature",
		"repo": "` + repoDir + `",
		"root": "` + rootDir + `",
		"created": "2024-01-01T00:00:00Z",
		"archived": true,
		"assistant": "old-assistant"
	}`
	if err := os.WriteFile(filepath.Join(archivedDir, "workspace.json"), []byte(archivedJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(archived) error = %v", err)
	}

	activeID := WorkspaceID("zzz-active-stale")
	activeDir := filepath.Join(storeRoot, string(activeID))
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(activeDir) error = %v", err)
	}
	activeJSON := `{
		"name": "feature",
		"repo": "` + repoDir + `",
		"root": "` + rootDir + `",
		"created": "2024-06-01T00:00:00Z",
		"assistant": "new-assistant"
	}`
	if err := os.WriteFile(filepath.Join(activeDir, "workspace.json"), []byte(activeJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(active) error = %v", err)
	}

	discovered := &Workspace{
		Name: "feature",
		Repo: repoDir,
		Root: rootDir,
	}

	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found stored metadata via fallback")
	}

	// Should have merged the active entry, not the archived one.
	if discovered.Archived {
		t.Error("expected non-archived workspace to be preferred")
	}
	if discovered.Assistant != "new-assistant" {
		t.Errorf("Assistant = %v, want 'new-assistant'", discovered.Assistant)
	}
}

func TestWorkspaceStore_ListByRepoCWDIndependent(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	// Create real directories.
	base := t.TempDir()
	repoDir := filepath.Join(base, "project")
	rootDir := filepath.Join(repoDir, ".amux", "workspaces", "ws1")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootDir) error = %v", err)
	}

	ws := &Workspace{
		Name:   "ws1",
		Branch: "main",
		Repo:   repoDir,
		Root:   rootDir,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	// Call ListByRepo from the repo directory.
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir(repoDir) error = %v", err)
	}
	list1, err := store.ListByRepo(repoDir)
	if err != nil {
		t.Fatalf("ListByRepo() from repoDir error = %v", err)
	}

	// Call ListByRepo from a different directory.
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("Chdir(otherDir) error = %v", err)
	}
	list2, err := store.ListByRepo(repoDir)
	if err != nil {
		t.Fatalf("ListByRepo() from otherDir error = %v", err)
	}

	// Restore original working directory.
	if err := os.Chdir(origDir); err != nil {
		t.Fatalf("Chdir(origDir) error = %v", err)
	}

	if len(list1) != len(list2) {
		t.Fatalf("ListByRepo results differ: %d vs %d", len(list1), len(list2))
	}
	if len(list1) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(list1))
	}
	if list1[0].Name != list2[0].Name {
		t.Errorf("workspace names differ: %q vs %q", list1[0].Name, list2[0].Name)
	}
}
