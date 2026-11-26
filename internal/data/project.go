package data

import (
	"path/filepath"
	"strings"
)

// Project represents a registered git repository with its workspaces
type Project struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"` // Absolute path to repository
	Workspaces []Workspace `json:"-"`    // Discovered dynamically via git
	// ShelvedWorkspaces are intentionally archived workspaces (worktree
	// removed, branch + metadata kept) awaiting restore or purge. Kept
	// separate from Workspaces so consumers iterating the live set never
	// see shelf entries; populated by the load path only.
	ShelvedWorkspaces []Workspace `json:"-"`
}

// ProjectNameForRepo derives a project's display name from a repo path —
// the basename, which is what NewProject stores in Project.Name. Kept as a
// helper so every consumer (dashboard rows, tmux display tags) derives the
// same label.
func ProjectNameForRepo(repoPath string) string {
	return filepath.Base(filepath.Clean(repoPath))
}

// NewProjectNamed creates a Project from a repository path and an explicit
// name — the registry's authoritative name for the project (which may be a
// disambiguated or renamed value, not the basename). An empty or unsafe
// name falls back to the basename, matching NewProject; the safety rule is
// the one law (isSafeProjectName).
func NewProjectNamed(name, path string) *Project {
	name = strings.TrimSpace(name)
	if !isSafeProjectName(name) {
		name = ProjectNameForRepo(path)
	}
	return &Project{
		Name:       name,
		Path:       path,
		Workspaces: []Workspace{},
	}
}

// NewProject creates a new Project from a repository path
func NewProject(path string) *Project {
	return &Project{
		Name:       ProjectNameForRepo(path),
		Path:       path,
		Workspaces: []Workspace{},
	}
}
