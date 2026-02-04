package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Registry manages the projects.json file for persistent project tracking
type Registry struct {
	path string
	mu   sync.RWMutex
}

// registryFile represents the JSON structure of projects.json
// Supports both legacy format (plain array) and new format (object with projects)
type registryFile struct {
	Projects []registryProject `json:"projects"`
}

// registryFileStrings is an alternate format where projects is just string paths
type registryFileStrings struct {
	Projects []string `json:"projects"`
}

type registryProject struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Profile string `json:"profile,omitempty"`
}

// NewRegistry creates a new registry at the specified path
func NewRegistry(path string) *Registry {
	return &Registry{
		path: path,
	}
}

// Load reads the project paths from the registry file
func (r *Registry) Load() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Try new format first: {"projects": [{name, path}, ...]}
	var registry registryFile
	if err := json.Unmarshal(data, &registry); err == nil {
		if len(registry.Projects) == 0 {
			return []string{}, nil
		}
		paths := make([]string, len(registry.Projects))
		for i, p := range registry.Projects {
			paths[i] = p.Path
		}
		return paths, nil
	}

	// Try alternate format: {"projects": ["path1", "path2"]}
	var registryStrings registryFileStrings
	if err := json.Unmarshal(data, &registryStrings); err == nil {
		return registryStrings.Projects, nil
	}

	// Fall back to legacy format: ["path1", "path2"]
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, err
	}

	return paths, nil
}

// Save writes the project paths to the registry file
func (r *Registry) Save(paths []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Build registry structure
	registry := registryFile{
		Projects: make([]registryProject, len(paths)),
	}
	for i, path := range paths {
		name := filepath.Base(path)
		registry.Projects[i] = registryProject{
			Name: name,
			Path: path,
		}
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, data, 0644)
}

// AddProject adds a project path to the registry
func (r *Registry) AddProject(path string) error {
	projects, err := r.LoadFull()
	if err != nil {
		return err
	}

	// Check if already exists
	for _, p := range projects {
		if p.Path == path {
			return nil // Already registered
		}
	}

	projects = append(projects, registryProject{
		Name: filepath.Base(path),
		Path: path,
	})
	return r.saveFull(projects)
}

// RemoveProject removes a project path from the registry
func (r *Registry) RemoveProject(path string) error {
	projects, err := r.LoadFull()
	if err != nil {
		return err
	}

	var filtered []registryProject
	for _, p := range projects {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}

	return r.saveFull(filtered)
}

// Projects returns a copy of all registered project paths
func (r *Registry) Projects() ([]string, error) {
	return r.Load()
}

// LoadFull reads the full project records (name, path, profile) from the registry file.
func (r *Registry) LoadFull() ([]registryProject, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return []registryProject{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Try structured format: {"projects": [{name, path, profile}, ...]}
	var registry registryFile
	if err := json.Unmarshal(raw, &registry); err == nil {
		if len(registry.Projects) == 0 {
			return []registryProject{}, nil
		}
		return registry.Projects, nil
	}

	// Try string array format: {"projects": ["path1", "path2"]}
	var registryStrings registryFileStrings
	if err := json.Unmarshal(raw, &registryStrings); err == nil {
		projects := make([]registryProject, len(registryStrings.Projects))
		for i, p := range registryStrings.Projects {
			projects[i] = registryProject{Name: filepath.Base(p), Path: p}
		}
		return projects, nil
	}

	// Legacy plain array: ["path1", "path2"]
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil, err
	}
	projects := make([]registryProject, len(paths))
	for i, p := range paths {
		projects[i] = registryProject{Name: filepath.Base(p), Path: p}
	}
	return projects, nil
}

// saveFull writes the full project records to the registry file.
func (r *Registry) saveFull(projects []registryProject) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if projects == nil {
		projects = []registryProject{}
	}
	registry := registryFile{Projects: projects}
	raw, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, raw, 0644)
}

// SetProfile sets the profile for a project identified by its path.
func (r *Registry) SetProfile(projectPath, profile string) error {
	projects, err := r.LoadFull()
	if err != nil {
		return err
	}

	for i := range projects {
		if projects[i].Path == projectPath {
			projects[i].Profile = profile
			return r.saveFull(projects)
		}
	}

	return fmt.Errorf("project not found: %s", projectPath)
}

// RenameProfile updates all projects using oldProfile to use newProfile.
func (r *Registry) RenameProfile(oldProfile, newProfile string) error {
	projects, err := r.LoadFull()
	if err != nil {
		return err
	}

	changed := false
	for i := range projects {
		if projects[i].Profile == oldProfile {
			projects[i].Profile = newProfile
			changed = true
		}
	}

	if changed {
		return r.saveFull(projects)
	}
	return nil
}

// ClearProfile clears the profile from all projects using the specified profile.
func (r *Registry) ClearProfile(profile string) error {
	projects, err := r.LoadFull()
	if err != nil {
		return err
	}

	changed := false
	for i := range projects {
		if projects[i].Profile == profile {
			projects[i].Profile = ""
			changed = true
		}
	}

	if changed {
		return r.saveFull(projects)
	}
	return nil
}
