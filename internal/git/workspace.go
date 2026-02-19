package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/medusa/internal/data"
)

// CreateWorkspace creates a new workspace backed by a git worktree
func CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	// Create branch from base and checkout into workspace path
	_, err := RunGit(repoPath, "worktree", "add", "--no-track", "-b", branch, workspacePath, base)
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
			return os.RemoveAll(workspacePath)
		}
		return err
	}
	// git worktree remove --force may leave the directory behind if it
	// contains untracked files (e.g. .claude/settings.local.json).
	// Clean up any leftover directory.
	if _, statErr := os.Stat(workspacePath); statErr == nil {
		return os.RemoveAll(workspacePath)
	}
	return nil
}

// DeleteBranch deletes a git branch
func DeleteBranch(repoPath, branch string) error {
	_, err := RunGit(repoPath, "branch", "-D", branch)
	return err
}

// MoveWorkspace moves a git worktree from oldPath to newPath.
func MoveWorkspace(repoPath, oldPath, newPath string) error {
	_, err := RunGit(repoPath, "worktree", "move", oldPath, newPath)
	return err
}

// RenameBranch renames a git branch from oldBranch to newBranch.
func RenameBranch(repoPath, oldBranch, newBranch string) error {
	_, err := RunGit(repoPath, "branch", "-m", oldBranch, newBranch)
	return err
}

// BranchExists returns true if the given branch exists in the repository.
func BranchExists(repoPath, branch string) bool {
	output, err := RunGit(repoPath, "branch", "--list", branch)
	if err != nil {
		return false
	}
	return strings.TrimSpace(output) != ""
}

// ResolveWorktreeRepo resolves a worktree (or plain clone) directory back to
// the path of the main repository that owns it.
// It first tries git directly, then falls back to parsing the .git file
// for cases where the worktree link is broken (e.g. worktree was moved/copied).
func ResolveWorktreeRepo(worktreePath string) (string, error) {
	// Try git first — works for healthy worktrees and normal clones
	commonDir, err := RunGit(worktreePath, "rev-parse", "--git-common-dir")
	if err == nil {
		commonDir = strings.TrimSpace(commonDir)
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(worktreePath, commonDir)
		}
		repoRoot := filepath.Dir(filepath.Clean(commonDir))
		if IsGitRepository(repoRoot) {
			return repoRoot, nil
		}
	}

	// Fallback: parse .git file directly for broken worktree links.
	// A worktree's .git file contains "gitdir: /path/to/repo/.git/worktrees/<name>".
	// We walk up from the gitdir to find the repo root.
	gitPath := filepath.Join(worktreePath, ".git")
	info, statErr := os.Stat(gitPath)
	if statErr != nil {
		return "", fmt.Errorf("resolve worktree repo %s: %w", worktreePath, err)
	}
	if info.IsDir() {
		// .git is a directory — this is a normal clone, not a worktree.
		// git already failed above, so this repo is broken.
		return "", fmt.Errorf("resolve worktree repo %s: %w", worktreePath, err)
	}

	raw, readErr := os.ReadFile(gitPath)
	if readErr != nil {
		return "", fmt.Errorf("read .git file %s: %w", gitPath, readErr)
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("invalid .git file in %s", worktreePath)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))

	// gitDir is like /path/to/repo/.git/worktrees/<name>
	// Walk up to find the .git dir, then its parent is the repo root.
	dir := gitDir
	for dir != "/" && dir != "." {
		base := filepath.Base(dir)
		dir = filepath.Dir(dir)
		if base == ".git" {
			// dir is now the repo root
			if IsGitRepository(dir) {
				return dir, nil
			}
			break
		}
	}

	return "", fmt.Errorf("could not resolve repo for worktree %s", worktreePath)
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
