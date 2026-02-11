package data

import (
	"encoding/json"
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

func TestRegistry_LoadLegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	// Write legacy format (plain array)
	legacyData := []string{"/path/to/project1", "/path/to/project2"}
	data, _ := json.Marshal(legacyData)
	_ = os.WriteFile(registryPath, data, 0o644)

	r := NewRegistry(registryPath)
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() legacy format error = %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths from legacy format, got %d", len(paths))
	}
}

func TestRegistry_LoadStringArrayFormat(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	// Write string array format
	data := `{"projects": ["/path/to/project1", "/path/to/project2"]}`
	_ = os.WriteFile(registryPath, []byte(data), 0o644)

	r := NewRegistry(registryPath)
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() string array format error = %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}
}
