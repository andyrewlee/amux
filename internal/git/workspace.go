package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

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

// DiscoverWorkspaces discovers git worktrees for a project.
// Returns workspaces with minimal fields populated (Name, Branch, Repo, Root).
// The caller should merge with stored metadata to get full workspace data.
func DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	output, err := RunGit(project.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktreeList(output, project.Path), nil
}

// parseWorktreeList parses the output of `git worktree list --porcelain`
func parseWorktreeList(output, repoPath string) []data.Workspace {
	var workspaces []data.Workspace
	var current struct {
		path   string
		branch string
		bare   bool
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			// End of entry, save if we have a path and it's not bare
			if current.path != "" && !current.bare {
				ws := data.Workspace{
					Name:   filepath.Base(current.path),
					Branch: current.branch,
					Repo:   repoPath,
					Root:   current.path,
				}
				workspaces = append(workspaces, ws)
			}
			current.path = ""
			current.branch = ""
			current.bare = false
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			// Format: "branch refs/heads/main"
			ref := strings.TrimPrefix(line, "branch ")
			current.branch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "bare" {
			current.bare = true
		}
	}

	// Handle last entry (if no trailing newline)
	if current.path != "" && !current.bare {
		ws := data.Workspace{
			Name:   filepath.Base(current.path),
			Branch: current.branch,
			Repo:   repoPath,
			Root:   current.path,
		}
		workspaces = append(workspaces, ws)
	}

	return workspaces
}
