package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry_LoadSave(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	r := NewRegistry(registryPath)

	// Initially empty
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("Expected empty paths, got %d", len(paths))
	}

	// Save some paths
	testPaths := []string{"/path/to/project1", "/path/to/project2"}
	if err := r.Save(testPaths); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load and verify
	loaded, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(loaded))
	}
	if loaded[0] != "/path/to/project1" {
		t.Errorf("Path[0] = %v, want /path/to/project1", loaded[0])
	}
}

func TestRegistryCreatesPrivateState(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "state")
	registryPath := filepath.Join(stateDir, "projects.json")

	r := NewRegistry(registryPath)
	if err := r.Save([]string{"/path/to/project"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	dirInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("Stat(state dir) error = %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected registry dir to be private, got mode %03o", mode)
	}
	lockInfo, err := os.Stat(registryPath + ".lock")
	if err != nil {
		t.Fatalf("Stat(lock file) error = %v", err)
	}
	if mode := lockInfo.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected registry lock to be private, got mode %03o", mode)
	}
}

func TestRegistry_AddProject(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	r := NewRegistry(registryPath)

	// Add first project
	if err := r.AddProject("/path/to/project1"); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	// Add second project
	if err := r.AddProject("/path/to/project2"); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	// Add duplicate (should be no-op)
	if err := r.AddProject("/path/to/project1"); err != nil {
		t.Fatalf("AddProject() duplicate error = %v", err)
	}

	paths, _ := r.Load()
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths after adding duplicate, got %d", len(paths))
	}
}

func TestRegistry_RemoveProject(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	r := NewRegistry(registryPath)
	_ = r.AddProject("/path/to/project1")
	_ = r.AddProject("/path/to/project2")

	// Remove one
	if err := r.RemoveProject("/path/to/project1"); err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}

	paths, _ := r.Load()
	if len(paths) != 1 {
		t.Errorf("Expected 1 path after removal, got %d", len(paths))
	}
	if paths[0] != "/path/to/project2" {
		t.Errorf("Wrong path remaining: %v", paths[0])
	}
}

func TestRegistry_SaveNormalizesAndDedupesPaths(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoReal := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.Save([]string{repoReal, filepath.Join(repoReal, "."), "   "}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 normalized path, got %d (%v)", len(paths), paths)
	}
	if canonicalProjectPath(paths[0]) != canonicalProjectPath(repoReal) {
		t.Fatalf("path = %q, want canonical %q", paths[0], repoReal)
	}
}

func TestRegistry_AddProjectUsesCanonicalIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoReal := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.AddProject(repoReal); err != nil {
		t.Fatalf("AddProject(repoReal) error = %v", err)
	}
	if err := r.AddProject(filepath.Join(repoReal, ".")); err != nil {
		t.Fatalf("AddProject(normalized duplicate) error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 canonical project after duplicate add, got %d (%v)", len(paths), paths)
	}
}

func TestRegistry_RemoveProjectUsesCanonicalIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoReal := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.AddProject(repoReal); err != nil {
		t.Fatalf("AddProject(repoReal) error = %v", err)
	}
	if err := r.RemoveProject(filepath.Join(repoReal, ".")); err != nil {
		t.Fatalf("RemoveProject(normalized path) error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected registry to be empty after canonical remove, got %v", paths)
	}
}

func TestRegistry_AddProjectRejectsEmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	r := NewRegistry(registryPath)

	if err := r.AddProject("   "); err == nil {
		t.Fatalf("expected AddProject to reject empty path")
	}
}

func TestRegistry_RemoveProjectRejectsEmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	r := NewRegistry(registryPath)
	if err := r.Save([]string{"/path/to/project"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := r.RemoveProject(" "); err == nil {
		t.Fatalf("expected RemoveProject to reject empty path")
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected registry unchanged after rejected remove, got %v", paths)
	}
}

func TestRegistry_LoadFallsBackToBackupWhenPrimaryCorruptWithoutMutatingPrimary(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	backupPath := registryPath + ".bak"
	brokenPrimary := []byte("{invalid json")

	if err := os.WriteFile(registryPath, brokenPrimary, 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	backupJSON := `{"projects":[{"name":"repo","path":"/path/to/repo"}]}`
	if err := os.WriteFile(backupPath, []byte(backupJSON), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	r := NewRegistry(registryPath)
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != "/path/to/repo" {
		t.Fatalf("unexpected backup load result: %v", paths)
	}

	primaryAfter, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read primary after load: %v", err)
	}
	if string(primaryAfter) != string(brokenPrimary) {
		t.Fatalf("expected Load() fallback to avoid mutating primary file")
	}
}

func TestRegistry_AddProjectDuplicateRepairsPrimaryFromBackup(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	backupPath := registryPath + ".bak"
	repoPath := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	if err := os.WriteFile(registryPath, []byte("{broken"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	backupJSON := `{"projects":[{"name":"repo","path":"` + repoPath + `"}]}`
	if err := os.WriteFile(backupPath, []byte(backupJSON), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.AddProject(repoPath); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	primaryData, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read repaired primary: %v", err)
	}
	paths, err := parseRegistryData(primaryData, registryPath)
	if err != nil {
		t.Fatalf("expected repaired primary to be parseable, got %v", err)
	}
	if len(paths) != 1 || canonicalProjectPath(paths[0].Path) != canonicalProjectPath(repoPath) {
		t.Fatalf("unexpected repaired primary data: %v", paths)
	}
}

func TestRegistry_RemoveProjectNoopRepairsPrimaryFromBackup(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	backupPath := registryPath + ".bak"
	repoPath := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	if err := os.WriteFile(registryPath, []byte("{broken"), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}
	backupJSON := `{"projects":[{"name":"repo","path":"` + repoPath + `"}]}`
	if err := os.WriteFile(backupPath, []byte(backupJSON), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.RemoveProject(filepath.Join(tmpDir, "missing")); err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}

	primaryData, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read repaired primary: %v", err)
	}
	paths, err := parseRegistryData(primaryData, registryPath)
	if err != nil {
		t.Fatalf("expected repaired primary to be parseable, got %v", err)
	}
	if len(paths) != 1 || canonicalProjectPath(paths[0].Path) != canonicalProjectPath(repoPath) {
		t.Fatalf("unexpected repaired primary data: %v", paths)
	}
}

func TestRegistry_AddProjectMintsUniqueNamesForSameBasename(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	r := NewRegistry(registryPath)
	if err := r.AddProject(filepath.Join(tmpDir, "Code", "checkfu")); err != nil {
		t.Fatalf("AddProject(first) error = %v", err)
	}
	if err := r.AddProject(filepath.Join(tmpDir, "other", "checkfu")); err != nil {
		t.Fatalf("AddProject(second) error = %v", err)
	}
	if err := r.AddProject(filepath.Join(tmpDir, "third", "checkfu")); err != nil {
		t.Fatalf("AddProject(third) error = %v", err)
	}

	entries, err := r.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	want := []string{"checkfu", "checkfu-2", "checkfu-3"}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %d (%v)", len(want), len(entries), entries)
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, name)
		}
	}
}

func TestRegistry_AddProjectMintSkipsCaseInsensitiveCollision(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	r := NewRegistry(registryPath)
	if err := r.AddProject(filepath.Join(tmpDir, "a", "Checkfu")); err != nil {
		t.Fatalf("AddProject(first) error = %v", err)
	}
	if err := r.AddProject(filepath.Join(tmpDir, "b", "checkfu")); err != nil {
		t.Fatalf("AddProject(second) error = %v", err)
	}

	entries, err := r.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if entries[0].Name != "Checkfu" || entries[1].Name != "checkfu-2" {
		t.Fatalf("expected Checkfu/checkfu-2 (case-insensitive segments), got %q/%q", entries[0].Name, entries[1].Name)
	}
}

func TestRegistry_EntriesRepairsDuplicateNamesFromLegacyFile(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	codeCheckfu := filepath.Join(tmpDir, "Code", "checkfu")
	homeCheckfu := filepath.Join(tmpDir, "home", "checkfu")
	legacy := `{"version":1,"projects":[` +
		`{"name":"checkfu","path":"` + codeCheckfu + `"},` +
		`{"name":"checkfu","path":"` + homeCheckfu + `"}]}`
	if err := os.WriteFile(registryPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy registry: %v", err)
	}

	r := NewRegistry(registryPath)
	entries, err := r.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d (%v)", len(entries), entries)
	}
	if entries[0].Name != "checkfu" || entries[1].Name != "checkfu-2" {
		t.Fatalf("expected duplicate names repaired to checkfu/checkfu-2, got %q/%q", entries[0].Name, entries[1].Name)
	}

	// The repair is in memory only; a mutating save persists it.
	if err := r.AddProject(filepath.Join(tmpDir, "extra", "repo")); err != nil {
		t.Fatalf("AddProject(extra) error = %v", err)
	}
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	persisted, err := parseRegistryData(raw, registryPath)
	if err != nil {
		t.Fatalf("parseRegistryData() error = %v", err)
	}
	if len(persisted) != 3 {
		t.Fatalf("expected 3 persisted entries, got %d", len(persisted))
	}
	if persisted[0].Name != "checkfu" || persisted[1].Name != "checkfu-2" {
		t.Fatalf("expected repaired names persisted, got %q/%q", persisted[0].Name, persisted[1].Name)
	}
}

func TestRegistry_RenameProject(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	codeCheckfu := filepath.Join(tmpDir, "Code", "checkfu")
	otherRepo := filepath.Join(tmpDir, "other", "repo")

	r := NewRegistry(registryPath)
	if err := r.AddProject(codeCheckfu); err != nil {
		t.Fatalf("AddProject(codeCheckfu) error = %v", err)
	}
	if err := r.AddProject(otherRepo); err != nil {
		t.Fatalf("AddProject(otherRepo) error = %v", err)
	}

	if err := r.RenameProject(codeCheckfu, "checkfu-old"); err != nil {
		t.Fatalf("RenameProject() error = %v", err)
	}
	entries, err := r.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if entries[0].Name != "checkfu-old" || entries[0].Path != codeCheckfu {
		t.Fatalf("expected rename to checkfu-old, got %+v", entries[0])
	}

	// Colliding rename (case-insensitive) is rejected.
	if err := r.RenameProject(otherRepo, "CHECKFU-OLD"); err == nil {
		t.Fatalf("expected collision with checkfu-old to be rejected (case-insensitive)")
	}

	// Unsafe names are rejected. (Trailing whitespace is trimmed first, so
	// the control-character case uses a non-whitespace control char.)
	for _, bad := range []string{"", "a/b", "..", "a\\b", "a\x01b"} {
		if err := r.RenameProject(codeCheckfu, bad); err == nil {
			t.Errorf("expected unsafe name %q rejected", bad)
		}
	}

	// Unknown path is rejected.
	if err := r.RenameProject(filepath.Join(tmpDir, "missing"), "nope"); err == nil {
		t.Fatalf("expected unknown project path to be rejected")
	}
}

func TestRegistry_SavePreservesStoredNames(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	codeCheckfu := filepath.Join(tmpDir, "Code", "checkfu")
	otherRepo := filepath.Join(tmpDir, "other", "repo")

	r := NewRegistry(registryPath)
	if err := r.AddProject(codeCheckfu); err != nil {
		t.Fatalf("AddProject(codeCheckfu) error = %v", err)
	}
	if err := r.AddProject(otherRepo); err != nil {
		t.Fatalf("AddProject(otherRepo) error = %v", err)
	}
	if err := r.RenameProject(codeCheckfu, "checkfu-old"); err != nil {
		t.Fatalf("RenameProject() error = %v", err)
	}

	// A path-only Save must not regenerate the basename over the rename.
	if err := r.Save([]string{otherRepo, codeCheckfu}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	entries, err := r.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for _, entry := range entries {
		if entry.Path == codeCheckfu && entry.Name != "checkfu-old" {
			t.Fatalf("Save() regenerated name %q over stored checkfu-old", entry.Name)
		}
	}
}
