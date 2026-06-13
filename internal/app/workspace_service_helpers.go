package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
)

func waitForGitPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("missing git metadata at %s after %s", path, timeout)
		}
		time.Sleep(gitPathWaitInterval)
	}
}

// cleanupStaleWorkspacePath removes a workspace directory only if it has no git metadata.
// Returns nil if the path does not exist (already cleaned). Returns an error if a .git
// file/directory is still present (workspace is not stale) or if removal fails.
func cleanupStaleWorkspacePath(workspacePath string) error {
	gitPath := filepath.Join(workspacePath, ".git")
	if _, err := os.Stat(gitPath); err == nil {
		return fmt.Errorf("workspace still has git metadata at %s", gitPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat git metadata at %s: %w", gitPath, err)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat workspace path %s: %w", workspacePath, err)
	}
	return os.RemoveAll(workspacePath)
}

// rollbackWorkspaceCreation undoes a partially-created workspace by removing the
// worktree and deleting the branch. It holds the per-repo git lock across both
// mutations so a rollback racing a concurrent same-repo create/delete cannot
// contend on git's .git locks (index.lock / packed-refs). Callers must NOT hold
// lockRepoGit(repoPath) when invoking this, or it self-deadlocks.
func (s *workspaceService) rollbackWorkspaceCreation(
	project *data.Project,
	repoPath, workspacePath, branch string,
) {
	unlock := s.lockRepoGit(repoPath)
	defer unlock()

	if err := s.gitOps.RemoveWorkspace(repoPath, workspacePath); err != nil {
		if git.IsUnregisteredWorkspacePathError(err) && isManagedWorkspacePathForProject(s.workspacesRoot, project, workspacePath) {
			cleanupErr := cleanupStaleWorkspacePath(workspacePath)
			if cleanupErr == nil {
				goto branchCleanup
			}
			err = errors.Join(err, cleanupErr)
		}
		logging.Warn("Failed to roll back workspace %s: %v", workspacePath, err)
	}
branchCleanup:
	if err := s.gitOps.DeleteBranch(repoPath, branch); err != nil {
		logging.Warn("Failed to roll back branch %s: %v", branch, err)
	}
}
