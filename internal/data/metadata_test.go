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

	result, err := store.Load(wt)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Warning != "" {
		t.Fatalf("expected no warning for default metadata, got %s", result.Warning)
	}

	meta := result.Metadata
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

func TestMetadataStoreLoadLegacyFilename(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "legacy-workspace",
		Branch: "legacy-workspace",
		Repo:   "/repo",
		Root:   "/worktrees/legacy-workspace",
		Base:   "main",
	}

	// Create metadata using legacy filename (worktree.json)
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacyContent := `{"name":"legacy-workspace","branch":"legacy-workspace","repo":"/repo","base":"main","assistant":"claude","script_mode":"concurrent"}`
	if err := os.WriteFile(filepath.Join(metaDir, "worktree.json"), []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Load should find and use the legacy file and return a warning
	result, err := store.Load(ws)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Warning == "" {
		t.Fatal("expected warning when loading from legacy file")
	}
	meta := result.Metadata
	if meta.Name != "legacy-workspace" {
		t.Fatalf("expected name 'legacy-workspace', got %s", meta.Name)
	}
	if meta.ScriptMode != "concurrent" {
		t.Fatalf("expected script_mode 'concurrent', got %s", meta.ScriptMode)
	}

	// Save should write to new filename (workspace.json)
	if err := store.Save(ws, meta); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify new file exists
	newPath := filepath.Join(metaDir, "workspace.json")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Fatal("expected workspace.json to be created after Save()")
	}

	// After save, Load should prefer the new file and have no warning
	result2, err := store.Load(ws)
	if err != nil {
		t.Fatalf("Load() after save error = %v", err)
	}
	if result2.Warning != "" {
		t.Fatalf("expected no warning after save, got %s", result2.Warning)
	}
	if result2.Metadata.Name != "legacy-workspace" {
		t.Fatalf("expected name 'legacy-workspace' after save, got %s", result2.Metadata.Name)
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

func TestMetadataStoreLoadLegacyPermissionError(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "legacy-permission-test",
		Branch: "legacy-permission-test",
		Repo:   "/repo",
		Root:   "/worktrees/legacy-permission-test",
	}

	// Create only legacy file with restricted permissions
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacyPath := filepath.Join(metaDir, "worktree.json")
	if err := os.WriteFile(legacyPath, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Make file unreadable
	if err := os.Chmod(legacyPath, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore permissions on cleanup
	t.Cleanup(func() {
		_ = os.Chmod(legacyPath, 0644)
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

	result, err := store.Load(wt)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loaded := result.Metadata
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

func TestMetadataStoreLoadBothFilesExist(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "both-exist",
		Branch: "both-exist",
		Repo:   "/repo",
		Root:   "/worktrees/both-exist",
	}

	// Create both files with different content
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	newContent := `{"name":"both-exist","assistant":"claude","script_mode":"nonconcurrent"}`
	legacyContent := `{"name":"both-exist","assistant":"codex","script_mode":"concurrent"}`
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte(newContent), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "worktree.json"), []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	// Load should prefer new file, no warning
	result, err := store.Load(ws)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Warning != "" {
		t.Fatalf("expected no warning when new file exists, got %s", result.Warning)
	}
	if result.Metadata.Assistant != "claude" {
		t.Fatalf("expected assistant 'claude' from new file, got %s", result.Metadata.Assistant)
	}
}

func TestMetadataStoreLoadNewCorruptedLegacyValid(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "corrupted-new",
		Branch: "corrupted-new",
		Repo:   "/repo",
		Root:   "/worktrees/corrupted-new",
	}

	// Create corrupted new file and valid legacy file
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corruptedContent := `{invalid json content}`
	legacyContent := `{"name":"corrupted-new","assistant":"codex","script_mode":"concurrent"}`
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte(corruptedContent), 0644); err != nil {
		t.Fatalf("write corrupted file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "worktree.json"), []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	// Load should fall back to legacy and return a warning
	result, err := store.Load(ws)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Warning == "" {
		t.Fatal("expected warning when recovering from corrupted new file")
	}
	if result.Metadata.Assistant != "codex" {
		t.Fatalf("expected assistant 'codex' from legacy file, got %s", result.Metadata.Assistant)
	}
}

func TestMetadataStoreLoadBothCorrupted(t *testing.T) {
	root := t.TempDir()
	store := NewMetadataStore(root)
	ws := &Workspace{
		Name:   "both-corrupted",
		Branch: "both-corrupted",
		Repo:   "/repo",
		Root:   "/worktrees/both-corrupted",
	}

	// Create both files with corrupted content
	metaDir := filepath.Join(root, string(ws.ID()))
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corruptedContent := `{invalid json content}`
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte(corruptedContent), 0644); err != nil {
		t.Fatalf("write corrupted new file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "worktree.json"), []byte(corruptedContent), 0644); err != nil {
		t.Fatalf("write corrupted legacy file: %v", err)
	}

	// Load should return an error
	_, err := store.Load(ws)
	if err == nil {
		t.Fatal("expected error when both files are corrupted")
	}
}
