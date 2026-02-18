package git

import (
	"fmt"
	"strings"
)

// BranchMode selects how the base branch is resolved when creating a workspace.
type BranchMode int

const (
	BranchModeRemoteMain BranchMode = iota // Fetch latest origin/<main>
	BranchModeCheckedOut                    // Use current local branch, no fetch
	BranchModeCustom                        // Resolve specific branch locally then remote
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

// ValidateRef checks that a ref resolves to a valid commit.
func ValidateRef(repoPath, ref string) error {
	_, err := RunGit(repoPath, "rev-parse", "--verify", ref+"^{commit}")
	return err
}

// GetFreshRemoteBase fetches if stale, then returns "origin/<base>" if it
// exists, falling back to the local base branch name.
func GetFreshRemoteBase(repoPath string) (string, error) {
	// Best-effort fetch; ignore errors (e.g. no network).
	_ = FetchIfStale(repoPath)

	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return base, err
	}

	remote := "origin/" + base
	if _, err := RunGit(repoPath, "rev-parse", "--verify", remote); err == nil {
		return remote, nil
	}
	return base, nil
}

// GetCheckedOutBase returns the current branch name for use as a worktree base.
// No fetch is performed.
func GetCheckedOutBase(repoPath string) (string, error) {
	return GetCurrentBranch(repoPath)
}

// ResolveCustomBranch looks up a branch locally first, then on the remote.
// Returns an error if neither is found.
func ResolveCustomBranch(repoPath, branch string) (string, error) {
	if BranchExists(repoPath, branch) {
		return branch, nil
	}
	remote := "origin/" + branch
	if ValidateRef(repoPath, remote) == nil {
		return remote, nil
	}
	return "", fmt.Errorf("branch %q not found locally or on remote", branch)
}

// ResolveCustomBranchWithFallback is like ResolveCustomBranch but falls back to
// GetFreshRemoteBase when the branch is not found. The usedFallback return
// value indicates whether the fallback was used. Used for grouped workspaces.
func ResolveCustomBranchWithFallback(repoPath, branch string) (string, bool, error) {
	ref, err := ResolveCustomBranch(repoPath, branch)
	if err == nil {
		return ref, false, nil
	}
	// Branch not found — fall back to remote main
	base, fbErr := GetFreshRemoteBase(repoPath)
	if fbErr != nil {
		return "", false, fmt.Errorf("branch %q not found and fallback failed: %w", branch, fbErr)
	}
	return base, true, nil
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
