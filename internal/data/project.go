package data

import (
	"path/filepath"
)

// Project represents a registered git repository with its workspaces
type Project struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"` // Absolute path to repository
	Workspaces []Workspace `json:"-"`    // Discovered dynamically via git
}

// NewProject creates a new Project from a repository path
func NewProject(path string) *Project {
	return &Project{
		Name:       filepath.Base(path),
		Path:       path,
		Workspaces: []Workspace{},
	}
}
