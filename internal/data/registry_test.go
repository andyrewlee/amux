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
	r.AddProject("/path/to/project1")
	r.AddProject("/path/to/project2")

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

func TestRegistry_LoadLegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	// Write legacy format (plain array)
	legacyData := []string{"/path/to/project1", "/path/to/project2"}
	data, _ := json.Marshal(legacyData)
	os.WriteFile(registryPath, data, 0644)

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
	os.WriteFile(registryPath, []byte(data), 0644)

	r := NewRegistry(registryPath)
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() string array format error = %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}
}
