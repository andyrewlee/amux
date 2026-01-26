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

// AddWorkspace adds a workspace to the project
func (p *Project) AddWorkspace(ws Workspace) {
	p.Workspaces = append(p.Workspaces, ws)
}

// FindWorkspace finds a workspace by its root path
func (p *Project) FindWorkspace(root string) *Workspace {
	for i := range p.Workspaces {
		if p.Workspaces[i].Root == root {
			return &p.Workspaces[i]
		}
	}
	return nil
}

// FindWorkspaceByName finds a workspace by its name
func (p *Project) FindWorkspaceByName(name string) *Workspace {
	for i := range p.Workspaces {
		if p.Workspaces[i].Name == name {
			return &p.Workspaces[i]
		}
	}
	return nil
}
