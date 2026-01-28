package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataStoreLoadDefault(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	wt := &Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/repo",
		Root:   "/worktrees/feature-1",
		Base:   "origin/main",
	}

	meta, err := store.Load(wt)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if meta.Name != wt.Name || meta.Branch != wt.Branch || meta.Repo != wt.Repo {
		t.Fatalf("Load() default metadata mismatch: %+v", meta)
	}
	if meta.Assistant != "claude" {
		t.Fatalf("expected default assistant 'claude', got %s", meta.Assistant)
	}
	if meta.ScriptMode != "nonconcurrent" {
		t.Fatalf("expected default script_mode 'nonconcurrent', got %s", meta.ScriptMode)
	}
	if meta.Env == nil {
		t.Fatal("expected default env map to be initialized")
	}
}

func TestMetadataStoreLoadMalformedJSON(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	wt := &Workspace{
		Name:   "feature-bad",
		Branch: "feature-bad",
		Repo:   "/repo",
		Root:   "/worktrees/feature-bad",
	}

	// Create malformed metadata file (note: file is named workspace.json)
	metaDir := filepath.Join(root, string(wt.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte(`{invalid json}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := store.Load(wt)
	if err == nil {
		t.Fatalf("Load() should fail for malformed JSON")
	}
}

func TestMetadataStoreDeleteNonExistent(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	wt := &Workspace{
		Name:   "nonexistent",
		Branch: "nonexistent",
		Repo:   "/repo",
		Root:   "/worktrees/nonexistent",
	}

	// Delete should not error for non-existent metadata
	if err := store.Delete(wt); err != nil {
		t.Fatalf("Delete() should not fail for non-existent metadata: %v", err)
	}
}

func TestMetadataStoreLoadPermissionError(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "permission-test",
		Branch: "permission-test",
		Repo:   "/repo",
		Root:   "/worktrees/permission-test",
	}

	// Create metadata file with restricted permissions
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	metaPath := filepath.Join(metaDir, "workspace.json")
	if err := os.WriteFile(metaPath, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Make file unreadable
	if err := os.Chmod(metaPath, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore permissions on cleanup
	t.Cleanup(func() {
		_ = os.Chmod(metaPath, 0644)
	})

	// Load should return a permission error, not fall back to defaults
	_, err := store.Load(ws)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected permission error, got IsNotExist: %v", err)
	}
}

func TestMetadataStoreSaveLoadDelete(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	wt := &Workspace{
		Name:   "feature-2",
		Branch: "feature-2",
		Repo:   "/repo",
		Root:   "/worktrees/feature-2",
		Base:   "main",
	}

	meta := &Metadata{
		Name:       "feature-2",
		Branch:     "feature-2",
		Repo:       "/repo",
		Base:       "main",
		Created:    "2025-01-01T00:00:00Z",
		Assistant:  "codex",
		ScriptMode: "concurrent",
		Env: map[string]string{
			"CUSTOM_VAR": "custom_value",
		},
		Scripts: ScriptsConfig{
			Run:     "echo run",
			Archive: "echo archive",
		},
	}

	if err := store.Save(wt, meta); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(wt)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Assistant != "codex" || loaded.ScriptMode != "concurrent" {
		t.Fatalf("Load() mismatch: %+v", loaded)
	}
	if loaded.Env["CUSTOM_VAR"] != "custom_value" {
		t.Fatalf("Load() env mismatch: %+v", loaded.Env)
	}

	if err := store.Delete(wt); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	metaDir := filepath.Join(root, string(wt.ID()))
	if _, err := os.Stat(metaDir); !os.IsNotExist(err) {
		t.Fatalf("expected metadata dir to be removed, got err=%v", err)
	}
}

