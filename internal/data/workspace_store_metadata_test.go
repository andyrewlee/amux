package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStore_LoadMetadataFor(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Simulate a discovered workspace (from git worktree discovery)
	discovered := &Workspace{
		Name:   "feature-branch",
		Branch: "feature-branch",
		Repo:   "/home/user/myrepo",
		Root:   "/home/user/.amux/workspaces/myrepo/feature-branch",
	}

	// Simulate stored metadata file (metadata fields only)
	// The ID is computed from Repo+Root, so we use discovered's ID
	id := discovered.ID()
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Stored metadata only had these fields (no Root, Repo, Name, Branch, Runtime)
	legacyMetadata := `{
		"created": "2024-06-15T14:30:00Z",
		"assistant": "codex",
		"script_mode": "concurrent",
		"env": {"API_KEY": "secret123"},
		"scripts": {"setup": "npm install"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(legacyMetadata), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// LoadMetadataFor should find and merge the stored metadata
	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found stored metadata")
	}

	// Verify discovered workspace kept its git info
	if discovered.Name != "feature-branch" {
		t.Errorf("Name = %v, want 'feature-branch'", discovered.Name)
	}
	if discovered.Branch != "feature-branch" {
		t.Errorf("Branch = %v, want 'feature-branch'", discovered.Branch)
	}
	if discovered.Repo != "/home/user/myrepo" {
		t.Errorf("Repo = %v, want '/home/user/myrepo'", discovered.Repo)
	}
	if discovered.Root != "/home/user/.amux/workspaces/myrepo/feature-branch" {
		t.Errorf("Root = %v, want '/home/user/.amux/workspaces/myrepo/feature-branch'", discovered.Root)
	}

	// Verify metadata was merged from stored file
	if discovered.Assistant != "codex" {
		t.Errorf("Assistant = %v, want 'codex'", discovered.Assistant)
	}
	if discovered.ScriptMode != "concurrent" {
		t.Errorf("ScriptMode = %v, want 'concurrent'", discovered.ScriptMode)
	}
	if discovered.Env["API_KEY"] != "secret123" {
		t.Errorf("Env[API_KEY] = %v, want 'secret123'", discovered.Env["API_KEY"])
	}
	if discovered.Scripts.Setup != "npm install" {
		t.Errorf("Scripts.Setup = %v, want 'npm install'", discovered.Scripts.Setup)
	}

	// Verify Created was parsed
	expectedTime := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	if !discovered.Created.Equal(expectedTime) {
		t.Errorf("Created = %v, want %v", discovered.Created, expectedTime)
	}
}

func TestWorkspaceStore_LoadMetadataFor_NotFound(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Workspace with no stored metadata
	ws := &Workspace{
		Name:   "new-workspace",
		Branch: "new-branch",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/repo/new-workspace",
	}

	found, err := store.LoadMetadataFor(ws)
	if err != nil {
		t.Errorf("LoadMetadataFor() error = %v, want nil for missing file", err)
	}
	if found {
		t.Error("LoadMetadataFor() should return false when no metadata exists")
	}
}

func TestWorkspaceStore_LoadMetadataFor_AppliesDefaults(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	discovered := &Workspace{
		Name:   "test-ws",
		Branch: "test",
		Repo:   "/repo",
		Root:   "/root",
	}

	// Store metadata with empty/missing fields
	id := discovered.ID()
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Metadata with empty assistant/script_mode
	emptyMetadata := `{
		"created": "2024-01-01T00:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(emptyMetadata), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found metadata")
	}

	// Verify defaults were applied
	if discovered.Assistant != "claude" {
		t.Errorf("Assistant = %v, want 'claude'", discovered.Assistant)
	}
	if discovered.ScriptMode != "nonconcurrent" {
		t.Errorf("ScriptMode = %v, want 'nonconcurrent'", discovered.ScriptMode)
	}
	if discovered.Env == nil {
		t.Error("Env should not be nil")
	}
	if discovered.Runtime != RuntimeLocalWorktree {
		t.Errorf("Runtime = %v, want %v", discovered.Runtime, RuntimeLocalWorktree)
	}
}

func TestWorkspaceStore_LoadMetadataFor_PreservesExistingAssistantWhenStoredEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	discovered := &Workspace{
		Name:      "test-ws",
		Branch:    "test",
		Repo:      "/repo",
		Root:      "/root",
		Assistant: "codex",
	}

	id := discovered.ID()
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	emptyMetadata := `{
		"created": "2024-01-01T00:00:00Z",
		"assistant": ""
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(emptyMetadata), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found metadata")
	}
	if discovered.Assistant != "codex" {
		t.Errorf("Assistant = %v, want 'codex'", discovered.Assistant)
	}
}

func TestWorkspaceStore_LoadMetadataFor_FallbackLookupPreservesExistingAssistantWhenStoredEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	store.SetDefaultAssistant("claude")

	discovered := &Workspace{
		Name:      "test-ws",
		Branch:    "test",
		Repo:      "/repo",
		Root:      "/root",
		Assistant: "codex",
	}

	legacyID := WorkspaceID("legacy_test_ws_id")
	dir := filepath.Join(root, string(legacyID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	legacyMetadata := `{
		"name": "test-ws",
		"branch": "test",
		"repo": "/repo",
		"root": "/root",
		"assistant": ""
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(legacyMetadata), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	found, err := store.LoadMetadataFor(discovered)
	if err != nil {
		t.Fatalf("LoadMetadataFor() error = %v", err)
	}
	if !found {
		t.Fatal("LoadMetadataFor() should have found metadata via fallback lookup")
	}
	if discovered.Assistant != "codex" {
		t.Errorf("Assistant = %v, want 'codex'", discovered.Assistant)
	}
}

func TestWorkspaceStore_UpsertFromDiscovery_PreservesDiscoveredAssistantWhenStoredEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	stored := &Workspace{
		Name:      "test-ws",
		Branch:    "test",
		Repo:      "/repo",
		Root:      "/root",
		Assistant: "",
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	discovered := &Workspace{
		Name:      "test-ws",
		Branch:    "test",
		Repo:      "/repo",
		Root:      "/root",
		Assistant: "codex",
	}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}

	loaded, err := store.Load(stored.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Assistant != "codex" {
		t.Errorf("Assistant = %v, want 'codex'", loaded.Assistant)
	}
}

func TestWorkspaceStore_UpsertFromDiscovery_StoreWinsAndClearsArchived(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Stored workspace carries user-owned metadata and is archived.
	stored := &Workspace{
		Name:       "kept-name",
		Branch:     "old-branch",
		Repo:       "/repo",
		Root:       "/root",
		Assistant:  "codex",
		Scripts:    ScriptsConfig{Setup: "npm install", Run: "npm start", Archive: "cleanup.sh"},
		ScriptMode: "concurrent",
		Env:        map[string]string{"API_KEY": "secret123"},
		OpenTabs: []TabInfo{
			{Assistant: "codex", Name: "codex", SessionName: "session-x", Status: "running"},
		},
		Archived:   true,
		ArchivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Discovery re-finds the same workspace (same Repo/Root → same ID) but with
	// a new Branch and no metadata.
	discovered := &Workspace{
		Name:   "discovered-name",
		Branch: "new-branch",
		Repo:   "/repo",
		Root:   "/root",
	}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}

	loaded, err := store.Load(stored.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Store-owned fields survive the rescan.
	if loaded.Scripts.Setup != "npm install" || loaded.Scripts.Run != "npm start" || loaded.Scripts.Archive != "cleanup.sh" {
		t.Errorf("Scripts = %#v, want stored scripts preserved", loaded.Scripts)
	}
	if loaded.ScriptMode != "concurrent" {
		t.Errorf("ScriptMode = %q, want concurrent (store-owned)", loaded.ScriptMode)
	}
	if loaded.Env["API_KEY"] != "secret123" {
		t.Errorf("Env[API_KEY] = %q, want secret123 (store-owned)", loaded.Env["API_KEY"])
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].SessionName != "session-x" {
		t.Errorf("OpenTabs = %#v, want stored tab preserved", loaded.OpenTabs)
	}
	if loaded.Assistant != "codex" {
		t.Errorf("Assistant = %q, want codex (store-owned)", loaded.Assistant)
	}
	if loaded.Name != "kept-name" {
		t.Errorf("Name = %q, want kept-name (store-owned when non-empty)", loaded.Name)
	}

	// Discovery updates Branch and clears Archived.
	if loaded.Branch != "new-branch" {
		t.Errorf("Branch = %q, want new-branch (discovery-updated)", loaded.Branch)
	}
	if loaded.Archived {
		t.Errorf("Archived = true, want cleared by discovery")
	}
	if !loaded.ArchivedAt.IsZero() {
		t.Errorf("ArchivedAt = %v, want zero after un-archive", loaded.ArchivedAt)
	}
}

func TestWorkspaceStore_UpsertFromDiscovery_RebindDeletesOldID(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	// Plant stored metadata under a legacy directory whose name is NOT the
	// canonical Repo+Root hash, so discovery is found via the fallback scan and
	// the recomputed ID differs (forcing the rebind-delete branch).
	legacyID := WorkspaceID("legacy_rebind_id")
	dir := filepath.Join(root, string(legacyID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	legacyMetadata := `{
		"name": "rebind-ws",
		"branch": "old-branch",
		"repo": "/repo",
		"root": "/root",
		"assistant": "codex",
		"env": {"API_KEY": "secret123"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(legacyMetadata), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	discovered := &Workspace{
		Name:   "rebind-ws",
		Branch: "new-branch",
		Repo:   "/repo",
		Root:   "/root",
	}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}

	// The recomputed canonical ID differs from the legacy one.
	newID := (Workspace{Repo: "/repo", Root: "/root"}).ID()
	if newID == legacyID {
		t.Fatalf("test setup: legacy and canonical IDs must differ")
	}

	// Exactly one workspace remains — the rebound canonical record. The old
	// legacy dir must be gone (no orphan).
	ids, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List() = %v, want exactly one workspace after rebind", ids)
	}
	if ids[0] != newID {
		t.Fatalf("remaining id = %s, want canonical %s", ids[0], newID)
	}

	// Store-owned metadata survives the rebind; Branch is discovery-updated.
	loaded, err := store.Load(newID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Env["API_KEY"] != "secret123" {
		t.Errorf("Env[API_KEY] = %q, want secret123 preserved across rebind", loaded.Env["API_KEY"])
	}
	if loaded.Branch != "new-branch" {
		t.Errorf("Branch = %q, want new-branch", loaded.Branch)
	}
}

func TestWorkspaceStore_LoadMetadataFor_CorruptedFile(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := &Workspace{
		Name:   "test-ws",
		Branch: "test",
		Repo:   "/repo",
		Root:   "/root",
	}

	// Create corrupted metadata file
	id := ws.ID()
	dir := filepath.Join(root, string(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write invalid JSON
	corruptedData := `{invalid json content`
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), []byte(corruptedData), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// LoadMetadataFor should return error for corrupted file
	found, err := store.LoadMetadataFor(ws)
	if err == nil {
		t.Error("LoadMetadataFor() should return error for corrupted metadata file")
	}
	if found {
		t.Error("LoadMetadataFor() should return found=false for corrupted file")
	}
}
