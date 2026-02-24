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

func TestRegistry_LoadLegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")

	// Write legacy format (plain array)
	legacyData := []string{"/path/to/project1", "/path/to/project2"}
	data, _ := json.Marshal(legacyData)
	_ = os.WriteFile(registryPath, data, 0644)

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
	_ = os.WriteFile(registryPath, []byte(data), 0644)

	r := NewRegistry(registryPath)
	paths, err := r.Load()
	if err != nil {
		t.Fatalf("Load() string array format error = %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(paths))
	}
}

func TestUpdateGroupRepos_PersistsWithProfile(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	r := NewRegistry(registryPath)

	// Add a group with a profile
	repos := []GroupRepo{{Path: "/repo/a", Name: "a"}}
	if err := r.AddGroup("mygroup", repos, "dev"); err != nil {
		t.Fatalf("AddGroup() error = %v", err)
	}

	// Update repos
	newRepos := []GroupRepo{{Path: "/repo/a", Name: "a"}, {Path: "/repo/b", Name: "b"}}
	if err := r.UpdateGroupRepos("mygroup", newRepos); err != nil {
		t.Fatalf("UpdateGroupRepos() error = %v", err)
	}

	// Verify profile is preserved
	groups, err := r.LoadGroups()
	if err != nil {
		t.Fatalf("LoadGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(groups))
	}
	if groups[0].Profile != "dev" {
		t.Errorf("Profile = %q, want %q", groups[0].Profile, "dev")
	}
	if len(groups[0].Repos) != 2 {
		t.Errorf("Repos count = %d, want 2", len(groups[0].Repos))
	}
}

func TestAddProject_PreservesGroups(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	r := NewRegistry(registryPath)

	// Add a group with a profile
	repos := []GroupRepo{{Path: "/repo/a", Name: "a"}}
	if err := r.AddGroup("mygroup", repos, "staging"); err != nil {
		t.Fatalf("AddGroup() error = %v", err)
	}

	// Add a project (uses saveFull which previously wiped groups)
	if err := r.AddProject("/path/to/project1"); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	// Verify groups survived
	groups, err := r.LoadGroups()
	if err != nil {
		t.Fatalf("LoadGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "mygroup" {
		t.Errorf("Name = %q, want %q", groups[0].Name, "mygroup")
	}
	if groups[0].Profile != "staging" {
		t.Errorf("Profile = %q, want %q", groups[0].Profile, "staging")
	}
}

func TestSave_PreservesGroups(t *testing.T) {
	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "projects.json")
	r := NewRegistry(registryPath)

	// Add a group with a profile
	repos := []GroupRepo{{Path: "/repo/a", Name: "a"}}
	if err := r.AddGroup("mygroup", repos, "prod"); err != nil {
		t.Fatalf("AddGroup() error = %v", err)
	}

	// Call Save (path-only save which previously wiped groups)
	if err := r.Save([]string{"/path/to/project1"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify groups survived
	groups, err := r.LoadGroups()
	if err != nil {
		t.Fatalf("LoadGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "mygroup" {
		t.Errorf("Name = %q, want %q", groups[0].Name, "mygroup")
	}
	if groups[0].Profile != "prod" {
		t.Errorf("Profile = %q, want %q", groups[0].Profile, "prod")
	}
}
