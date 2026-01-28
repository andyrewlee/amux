package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// workspaceEntry represents a raw workspace entry from git
type workspaceEntry struct {
	Path   string
	Head   string
	Branch string
	Bare   bool
}

// listWorkspaces lists all workspaces for a repository
func listWorkspaces(repoPath string) ([]workspaceEntry, error) {
	output, err := RunGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorkspaceList(output), nil
}

// parseWorkspaceList parses the porcelain output of git worktree list
func parseWorkspaceList(output string) []workspaceEntry {
	var entries []workspaceEntry
	var current workspaceEntry

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				entries = append(entries, current)
				current = workspaceEntry{}
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			// Branch is in format refs/heads/branch-name
			branch := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case line == "bare":
			current.Bare = true
		}
	}

	// Don't forget the last entry
	if current.Path != "" {
		entries = append(entries, current)
	}

	return entries
}

// DiscoverWorkspaces discovers all workspaces for a project and converts them to data.Workspace
func DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	entries, err := listWorkspaces(project.Path)
	if err != nil {
		return nil, err
	}

	var workspaces []data.Workspace
	for _, entry := range entries {
		if entry.Bare {
			continue
		}

		ws := data.Workspace{
			Name:   filepath.Base(entry.Path),
			Branch: entry.Branch,
			Repo:   project.Path,
			Root:   entry.Path,
			// Base and Created will be populated from metadata if available
		}

		workspaces = append(workspaces, ws)
	}

	return workspaces, nil
}

// CreateWorkspace creates a new workspace backed by a git worktree
func CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	// Create branch from base and checkout into workspace path
	_, err := RunGit(repoPath, "worktree", "add", "-b", branch, workspacePath, base)
	return err
}

// RemoveWorkspace removes a workspace backed by a git worktree
func RemoveWorkspace(repoPath, workspacePath string) error {
	_, err := RunGit(repoPath, "worktree", "remove", workspacePath, "--force")
	if err != nil {
		// git worktree remove --force unregisters the workspace (removes .git file)
		// but fails to delete the directory if it contains untracked files.
		// If the .git file is gone, the workspace was successfully unregistered
		// and we can safely remove the remaining directory ourselves.
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); os.IsNotExist(statErr) {
			// Workspace was unregistered, clean up leftover directory
			if removeErr := os.RemoveAll(workspacePath); removeErr != nil {
				return removeErr
			}
			return nil
		}
		return err
	}
	return nil
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	_, err := RunGit(repoPath, "branch", "-D", branch)
	return err
}
