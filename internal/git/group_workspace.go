package git

import (
	"fmt"
	"strings"

	"github.com/andyrewlee/medusa/internal/logging"
)

// RepoSpec describes how to create a worktree for one repo within a group.
type RepoSpec struct {
	RepoPath      string // main repo path
	RepoName      string // short name
	WorkspacePath string // target worktree directory
	Branch        string // branch name to create
	Base          string // base ref to branch from
}

// ValidateBranchAcrossRepos checks that the branch doesn't exist in any repo.
func ValidateBranchAcrossRepos(branch string, repoPaths []string) error {
	var conflicts []string
	for _, repoPath := range repoPaths {
		output, err := RunGit(repoPath, "branch", "--list", branch)
		if err != nil {
			continue
		}
		if strings.TrimSpace(output) != "" {
			conflicts = append(conflicts, repoPath)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("branch %q already exists in: %s", branch, strings.Join(conflicts, ", "))
	}
	return nil
}

// GetDefaultBase returns the default branch name for a repo (main, master, etc.).
func GetDefaultBase(repoPath string) (string, error) {
	return GetBaseBranch(repoPath)
}

// CreateGroupWorkspace creates worktrees for all repos.
// Rolls back on partial failure (removes successful worktrees).
func CreateGroupWorkspace(specs []RepoSpec) error {
	var created []int // indices of successfully created worktrees

	for i, spec := range specs {
		err := CreateWorkspace(spec.RepoPath, spec.WorkspacePath, spec.Branch, spec.Base)
		if err != nil {
			// Rollback: remove worktrees created so far
			for j := len(created) - 1; j >= 0; j-- {
				idx := created[j]
				s := specs[idx]
				if rmErr := RemoveWorkspace(s.RepoPath, s.WorkspacePath); rmErr != nil {
					logging.Warn("Rollback: failed to remove worktree %s: %v", s.WorkspacePath, rmErr)
				}
				if brErr := DeleteBranch(s.RepoPath, s.Branch); brErr != nil {
					logging.Warn("Rollback: failed to delete branch %s in %s: %v", s.Branch, s.RepoPath, brErr)
				}
			}
			return fmt.Errorf("failed to create worktree for %s: %w", spec.RepoName, err)
		}
		created = append(created, i)
	}

	return nil
}

// RemoveGroupWorkspace removes all worktrees and branches.
// Tolerates individual failures (logs warnings, continues).
func RemoveGroupWorkspace(specs []RepoSpec) []error {
	var errs []error
	for _, spec := range specs {
		if err := RemoveWorkspace(spec.RepoPath, spec.WorkspacePath); err != nil {
			logging.Warn("Failed to remove worktree %s: %v", spec.WorkspacePath, err)
			errs = append(errs, err)
		}
		if err := DeleteBranch(spec.RepoPath, spec.Branch); err != nil {
			logging.Warn("Failed to delete branch %s in %s: %v", spec.Branch, spec.RepoPath, err)
			// Don't add branch deletion failures to errs - worktree removal is primary
		}
	}
	return errs
}
