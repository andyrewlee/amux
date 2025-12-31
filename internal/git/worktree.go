package git

import (
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// WorktreeEntry represents a raw worktree entry from git
type WorktreeEntry struct {
	Path   string
	Head   string
	Branch string
	Bare   bool
}

// ListWorktrees lists all worktrees for a repository
func ListWorktrees(repoPath string) ([]WorktreeEntry, error) {
	output, err := RunGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktreeList(output), nil
}

// parseWorktreeList parses the porcelain output of git worktree list
func parseWorktreeList(output string) []WorktreeEntry {
	var entries []WorktreeEntry
	var current WorktreeEntry

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				entries = append(entries, current)
				current = WorktreeEntry{}
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

// DiscoverWorktrees discovers all worktrees for a project and converts them to data.Worktree
func DiscoverWorktrees(project *data.Project) ([]data.Worktree, error) {
	entries, err := ListWorktrees(project.Path)
	if err != nil {
		return nil, err
	}

	var worktrees []data.Worktree
	for _, entry := range entries {
		if entry.Bare {
			continue
		}

		wt := data.Worktree{
			Name:   filepath.Base(entry.Path),
			Branch: entry.Branch,
			Repo:   project.Path,
			Root:   entry.Path,
			// Base and Created will be populated from metadata if available
		}

		worktrees = append(worktrees, wt)
	}

	return worktrees, nil
}

// CreateWorktree creates a new git worktree
func CreateWorktree(repoPath, worktreePath, branch, base string) error {
	// Create branch from base and checkout into worktree path
	_, err := RunGit(repoPath, "worktree", "add", "-b", branch, worktreePath, base)
	return err
}

// RemoveWorktree removes a git worktree
func RemoveWorktree(repoPath, worktreePath string) error {
	_, err := RunGit(repoPath, "worktree", "remove", worktreePath, "--force")
	return err
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	_, err := RunGit(repoPath, "branch", "-D", branch)
	return err
}
