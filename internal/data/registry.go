package data

import (
	"encoding/json"
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
	Name string `json:"name"`
	Path string `json:"path"`
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
	if err := json.Unmarshal(data, &registry); err == nil && len(registry.Projects) > 0 {
		paths := make([]string, len(registry.Projects))
		for i, p := range registry.Projects {
			paths[i] = p.Path
		}
		return paths, nil
	}

	// Try alternate format: {"projects": ["path1", "path2"]}
	var registryStrings registryFileStrings
	if err := json.Unmarshal(data, &registryStrings); err == nil && len(registryStrings.Projects) > 0 {
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
	paths, err := r.Load()
	if err != nil {
		return err
	}

	// Check if already exists
	for _, p := range paths {
		if p == path {
			return nil // Already registered
		}
	}

	paths = append(paths, path)
	return r.Save(paths)
}

// RemoveProject removes a project path from the registry
func (r *Registry) RemoveProject(path string) error {
	paths, err := r.Load()
	if err != nil {
		return err
	}

	// Filter out the path
	var newPaths []string
	for _, p := range paths {
		if p != path {
			newPaths = append(newPaths, p)
		}
	}

	return r.Save(newPaths)
}

// Projects returns a copy of all registered project paths
func (r *Registry) Projects() ([]string, error) {
	return r.Load()
}
