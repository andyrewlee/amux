package data

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestRegistry_AddProjectDedupesSymlinkAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are unstable on windows test environments")
	}

	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoReal := filepath.Join(tmpDir, "repo-real")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}
	repoLink := filepath.Join(tmpDir, "repo-link")
	if err := os.Symlink(repoReal, repoLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.AddProject(repoReal); err != nil {
		t.Fatalf("AddProject(repoReal) error = %v", err)
	}
	if err := r.AddProject(repoLink); err != nil {
		t.Fatalf("AddProject(repoLink) error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 canonical project after symlink alias add, got %d (%v)", len(paths), paths)
	}
}

func TestRegistry_RemoveProjectMatchesSymlinkAliasWhenLeafMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are unstable on windows test environments")
	}

	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoParentReal := filepath.Join(tmpDir, "real-parent")
	if err := os.MkdirAll(repoParentReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoParentReal) error = %v", err)
	}
	repoParentLink := filepath.Join(tmpDir, "link-parent")
	if err := os.Symlink(repoParentReal, repoParentLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	removedLeaf := filepath.Join(repoParentReal, "repo-missing")
	linkLeaf := filepath.Join(repoParentLink, "repo-missing")

	r := NewRegistry(registryPath)
	if err := r.Save([]string{linkLeaf}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := r.RemoveProject(removedLeaf); err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected registry to be empty after alias remove on missing leaf, got %v", paths)
	}
}

func TestRegistry_RemoveProjectMatchesDirectSymlinkAliasAfterTargetDeletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are unstable on windows test environments")
	}

	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	repoReal := filepath.Join(tmpDir, "repo-real")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}
	repoLink := filepath.Join(tmpDir, "repo-link")
	if err := os.Symlink(repoReal, repoLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	r := NewRegistry(registryPath)
	if err := r.AddProject(repoLink); err != nil {
		t.Fatalf("AddProject(repoLink) error = %v", err)
	}

	if err := os.RemoveAll(repoReal); err != nil {
		t.Fatalf("RemoveAll(repoReal) error = %v", err)
	}
	if err := r.RemoveProject(repoLink); err != nil {
		t.Fatalf("RemoveProject(repoLink) error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected registry to be empty after removing broken symlink alias, got %v", paths)
	}
}

func TestRegistry_AddProjectDedupesRelativeSymlinkViaSymlinkedParentAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are unstable on windows test environments")
	}

	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	realRoot := filepath.Join(tmpDir, "real")
	realSub := filepath.Join(realRoot, "sub")
	repoReal := filepath.Join(realRoot, "repo-real")
	if err := os.MkdirAll(realSub, 0o755); err != nil {
		t.Fatalf("MkdirAll(realSub) error = %v", err)
	}
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}

	repoLink := filepath.Join(realSub, "repo-link")
	if err := os.Symlink("../repo-real", repoLink); err != nil {
		t.Fatalf("Symlink(repo-link) error = %v", err)
	}
	aliasDir := filepath.Join(tmpDir, "alias")
	if err := os.Symlink(realSub, aliasDir); err != nil {
		t.Fatalf("Symlink(alias) error = %v", err)
	}
	aliasLink := filepath.Join(aliasDir, "repo-link")

	r := NewRegistry(registryPath)
	if err := r.AddProject(aliasLink); err != nil {
		t.Fatalf("AddProject(aliasLink) error = %v", err)
	}
	if err := r.AddProject(repoReal); err != nil {
		t.Fatalf("AddProject(repoReal) error = %v", err)
	}

	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 canonical project after relative symlink alias add, got %d (%v)", len(paths), paths)
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
	if len(paths) != 1 || canonicalProjectPath(paths[0]) != canonicalProjectPath(repoPath) {
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
	if len(paths) != 1 || canonicalProjectPath(paths[0]) != canonicalProjectPath(repoPath) {
		t.Fatalf("unexpected repaired primary data: %v", paths)
	}
}
