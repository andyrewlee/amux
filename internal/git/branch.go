package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetBaseBranch returns the base branch (main, master, or the default branch)
func GetBaseBranch(repoPath string) (string, error) {
	// Try common base branch names in order of preference
	candidates := []string{"main", "master", "develop", "dev"}

	for _, branch := range candidates {
		// Check if branch exists
		_, err := RunGit(repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	// Try to get the default branch from remote
	output, err := RunGit(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		parts := strings.Split(output, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fall back to "main" if nothing else works
	return "main", nil
}

// GetBranchFileDiff returns the full diff for a single file on the branch
func GetBranchFileDiff(repoPath, path string) (*DiffResult, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}

	mergeBase, err := RunGit(repoPath, "merge-base", base, "HEAD")
	if err != nil {
		mergeBase = base
	}

	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", mergeBase + "...HEAD", "--", path}
	output, err := RunGit(repoPath, args...)
	if err != nil {
		return &DiffResult{
			Path:  path,
			Error: err.Error(),
		}, nil
	}

	return parseDiff(path, output), nil
}

// AheadBehind returns how many commits branch is ahead/behind base.
func AheadBehind(repoPath, base, branch string) (ahead, behind int, err error) {
	if repoPath == "" || base == "" || branch == "" {
		return 0, 0, nil
	}
	out, err := RunGit(repoPath, "rev-list", "--left-right", "--count", base+"..."+branch)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) < 2 {
		return 0, 0, nil
	}
	behind, _ = strconv.Atoi(parts[0])
	ahead, _ = strconv.Atoi(parts[1])
	return ahead, behind, nil
}

// RebaseInProgress reports whether a rebase is in progress.
func RebaseInProgress(repoPath string) bool {
	if repoPath == "" {
		return false
	}
	paths := []string{"rebase-merge", "rebase-apply"}
	for _, p := range paths {
		path, err := RunGit(repoPath, "rev-parse", "--git-path", p)
		if err != nil || path == "" {
			continue
		}
		// git rev-parse --git-path can return a relative path; resolve against repo
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoPath, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
