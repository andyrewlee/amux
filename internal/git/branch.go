package git

import (
	"context"
	"errors"
	"strings"
	"time"
)

const branchDiffTimeout = 15 * time.Second

// GetBaseBranch returns the base branch (main, master, or the default branch).
// All returned branches are verified to exist locally. Returns an error if no
// default branch can be determined.
func GetBaseBranch(repoPath string) (string, error) {
	// Try common base branch names in order of preference
	candidates := []string{"main", "master", "develop", "dev"}

	for _, branch := range candidates {
		_, err := RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	// Try to get the default branch from remote
	output, err := RunGitCtx(context.Background(), repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		parts := strings.Split(output, "/")
		branch := parts[len(parts)-1]
		// Verify the remote default branch exists locally
		_, err := RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	return "", errors.New("unable to determine default branch")
}

// GetBranchFileDiff returns the full diff for a single file on the branch
func GetBranchFileDiff(repoPath, path string) (*DiffResult, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), branchDiffTimeout)
	mergeBase, err := RunGitCtx(ctx, repoPath, "merge-base", base, "HEAD")
	cancel()
	if err != nil {
		mergeBase = base
	}

	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", mergeBase + "...HEAD", "--", path}
	ctx, cancel = context.WithTimeout(context.Background(), branchDiffTimeout)
	defer cancel()
	output, err := RunGitCtx(ctx, repoPath, args...)
	if err != nil {
		return &DiffResult{
			Path:  path,
			Error: err.Error(),
		}, nil
	}

	return parseDiff(path, output), nil
}
