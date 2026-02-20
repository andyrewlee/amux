package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

const worktreeTimeout = 30 * time.Second

var runGitCtx = RunGitCtx

// CreateWorkspace creates a new workspace backed by a git worktree
func CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	// Create branch from base and checkout into workspace path
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	_, err := runGitCtx(ctx, repoPath, "worktree", "add", "-b", branch, workspacePath, base)
	cancel()
	if err == nil {
		return nil
	}
	if !isBranchAlreadyExistsError(err, branch) {
		return err
	}

	// If the branch already exists, reuse it instead of failing hard.
	// Retry with a fresh timeout context so a slow first attempt does not
	// consume the entire budget for the fallback path.
	retryCtx, retryCancel := context.WithTimeout(context.Background(), worktreeTimeout)
	_, retryErr := runGitCtx(retryCtx, repoPath, "worktree", "add", workspacePath, branch)
	retryCancel()
	if retryErr != nil {
		firstErrMsg := err.Error()
		return fmt.Errorf(
			"worktree add with new branch failed: %s; fallback add existing branch failed: %w",
			firstErrMsg,
			retryErr,
		)
	}
	return nil
}

func isBranchAlreadyExistsError(err error, branch string) bool {
	if err == nil {
		return false
	}
	branch = strings.ToLower(strings.TrimSpace(branch))
	if branch == "" {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "a branch named '"+branch+"' already exists") {
		return true
	}
	if strings.Contains(msg, "a branch named `"+branch+"` already exists") {
		return true
	}
	if strings.Contains(msg, "branch '"+branch+"' already exists") {
		return true
	}
	if strings.Contains(msg, "branch `"+branch+"` already exists") {
		return true
	}
	return strings.Contains(msg, "already exists") && strings.Contains(msg, branch)
}

// RemoveWorkspace removes a workspace backed by a git worktree
func RemoveWorkspace(repoPath, workspacePath string) error {
	if !isRegisteredWorktree(repoPath, workspacePath) {
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); statErr == nil {
			return fmt.Errorf("workspace %s has a .git file but is not a registered worktree", workspacePath)
		}
		// Already removed externally — idempotent success.
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	_, err := runGitCtx(ctx, repoPath, "worktree", "remove", workspacePath, "--force")
	if err != nil {
		// git worktree remove --force unregisters the workspace (removes .git file)
		// but fails to delete the directory if it contains untracked files.
		// If the .git file is gone, the workspace was successfully unregistered
		// and we can safely remove the remaining directory ourselves.
		gitFile := filepath.Join(workspacePath, ".git")
		if _, statErr := os.Stat(gitFile); os.IsNotExist(statErr) {
			if !isSafeWorkspaceCleanupPath(workspacePath) {
				return fmt.Errorf("refusing to remove unsafe path: %s", workspacePath)
			}
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

func isRegisteredWorktree(repoPath, workspacePath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	output, _ := runGitCtx(ctx, repoPath, "worktree", "list", "--porcelain")
	// Resolve the workspace path to handle symlinks (e.g. macOS /var -> /private/var).
	normalized := workspacePath
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(workspacePath)); err == nil {
		normalized = filepath.Join(resolved, filepath.Base(workspacePath))
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "worktree ") {
			wtPath := strings.TrimPrefix(trimmed, "worktree ")
			if wtPath == workspacePath || wtPath == normalized {
				return true
			}
		}
	}
	return false
}

func isSafeWorkspaceCleanupPath(path string) bool {
	if path == "" {
		return false
	}
	cleaned := filepath.Clean(path)
	if cleaned == "/" || cleaned == "." {
		return false
	}
	home, err := os.UserHomeDir()
	if err == nil && cleaned == filepath.Clean(home) {
		return false
	}
	return true
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	_, err := runGitCtx(ctx, repoPath, "branch", "-D", branch)
	return err
}

// DiscoverWorkspaces discovers git worktrees for a project.
// Returns workspaces with minimal fields populated (Name, Branch, Repo, Root).
// The caller should merge with stored metadata to get full workspace data.
func DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeTimeout)
	defer cancel()
	output, err := runGitCtx(ctx, project.Path, "worktree", "list", "--porcelain")
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
