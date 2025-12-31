package data

import (
	"path/filepath"
)

// Project represents a registered git repository with its worktrees
type Project struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"` // Absolute path to repository
	Worktrees []Worktree `json:"-"`    // Discovered dynamically via git
}

// NewProject creates a new Project from a repository path
func NewProject(path string) *Project {
	return &Project{
		Name:      filepath.Base(path),
		Path:      path,
		Worktrees: []Worktree{},
	}
}

// AddWorktree adds a worktree to the project
func (p *Project) AddWorktree(wt Worktree) {
	p.Worktrees = append(p.Worktrees, wt)
}

// FindWorktree finds a worktree by its root path
func (p *Project) FindWorktree(root string) *Worktree {
	for i := range p.Worktrees {
		if p.Worktrees[i].Root == root {
			return &p.Worktrees[i]
		}
	}
	return nil
}

// FindWorktreeByName finds a worktree by its name
func (p *Project) FindWorktreeByName(name string) *Worktree {
	for i := range p.Worktrees {
		if p.Worktrees[i].Name == name {
			return &p.Worktrees[i]
		}
	}
	return nil
}
