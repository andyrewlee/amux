package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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

func rollbackWorkspaceCreation(repoPath, workspacePath, branch string) {
	if err := removeWorkspaceFn(repoPath, workspacePath); err != nil {
		logging.Warn("Failed to roll back workspace %s: %v", workspacePath, err)
	}
	if err := deleteBranchFn(repoPath, branch); err != nil {
		logging.Warn("Failed to roll back branch %s: %v", branch, err)
	}
}

func cleanupStaleWorkspacePath(workspacePath string) error {
	gitPath := filepath.Join(workspacePath, ".git")
	if _, err := os.Stat(gitPath); err == nil {
		return fmt.Errorf("workspace git metadata still present at %s", gitPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat workspace git metadata %s: %w", gitPath, err)
	}
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to stat workspace path %s: %w", workspacePath, err)
	}
	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("failed to remove stale workspace path %s: %w", workspacePath, err)
	}
	return nil
}
