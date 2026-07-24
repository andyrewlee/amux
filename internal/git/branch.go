package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
		_, err := RunGitCtx(WithRefreshSlot(context.Background()), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	// Try remote tracking branches for common candidates
	for _, branch := range candidates {
		remote := "origin/" + branch
		_, err := RunGitCtx(WithRefreshSlot(context.Background()), repoPath, "rev-parse", "--verify", remote)
		if err == nil {
			return remote, nil
		}
	}

	// Try to get the default branch from remote
	output, err := RunGitCtx(WithRefreshSlot(context.Background()), repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main" or "refs/remotes/origin/feature/foo"
		branch := strings.TrimPrefix(output, "refs/remotes/origin/")
		// Verify the branch exists locally
		_, err := RunGitCtx(WithRefreshSlot(context.Background()), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
		// Try remote tracking branch for symbolic-ref result
		remote := "origin/" + branch
		_, err = RunGitCtx(WithRefreshSlot(context.Background()), repoPath, "rev-parse", "--verify", remote)
		if err == nil {
			return remote, nil
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
	mergeBase := resolveMergeBase(repoPath, base)

	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", mergeBase + "...HEAD", "--", path}
	ctx, cancel := context.WithTimeout(WithRefreshSlot(context.Background()), branchDiffTimeout)
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

// resolveMergeBase returns merge-base(base, HEAD), falling back to base
// itself if the merge-base lookup fails (e.g. unrelated histories). Shared by
// GetBranchFileDiff and BranchChangesVsBase so the two stay consistent about
// which commit "vs base" means.
func resolveMergeBase(repoPath, base string) string {
	ctx, cancel := context.WithTimeout(WithRefreshSlot(context.Background()), branchDiffTimeout)
	defer cancel()
	mergeBase, err := RunGitCtx(ctx, repoPath, "merge-base", base, "HEAD")
	if err != nil {
		return base
	}
	return mergeBase
}

// BranchChangesVsBase lists every file that differs between HEAD and
// merge-base(base, HEAD) — i.e. everything committed on this branch that
// hasn't landed on base yet. It reuses the same base/merge-base resolution as
// GetBranchFileDiff (GetBaseBranch, then resolveMergeBase) so "vs base" means
// the same thing in both places. Read-only: no fetch, merge, or checkout.
func BranchChangesVsBase(repoPath string) ([]Change, error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return nil, err
	}
	mergeBase := resolveMergeBase(repoPath, base)

	ctx, cancel := context.WithTimeout(WithRefreshSlot(context.Background()), branchDiffTimeout)
	defer cancel()
	output, err := RunGitCtx(ctx, repoPath, "diff", "--no-color", "--name-status", mergeBase+"...HEAD")
	if err != nil {
		return nil, err
	}

	return parseNameStatus(output), nil
}

// parseNameStatus parses `git diff --name-status` output (one "CODE\tpath" or
// "CODE\toldpath\tnewpath" line per change) into Changes, reusing the same
// status-code mapping as working-tree status parsing.
func parseNameStatus(output string) []Change {
	if output == "" {
		return nil
	}
	var changes []Change
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		code := parts[0]
		change := Change{Kind: statusCodeToKind(code[0])}
		if (code[0] == 'R' || code[0] == 'C') && len(parts) >= 3 {
			change.OldPath = parts[1]
			change.Path = parts[2]
		} else {
			change.Path = parts[1]
		}
		changes = append(changes, change)
	}
	sortChanges(changes)
	return changes
}

// AheadBehind reports how many commits HEAD is ahead of and behind the
// workspace's base branch, using the same base resolution as
// GetBranchFileDiff/BranchChangesVsBase (GetBaseBranch) so all three agree on
// what "base" means for a workspace. Read-only. When no base branch can be
// determined (e.g. no candidate/remote branch exists), the GetBaseBranch
// error is returned unchanged so callers can tell "nothing to compare
// against" apart from a real git failure.
func AheadBehind(repoPath string) (ahead, behind int, err error) {
	base, err := GetBaseBranch(repoPath)
	if err != nil {
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(WithRefreshSlot(context.Background()), branchDiffTimeout)
	defer cancel()
	output, err := RunGitCtx(ctx, repoPath, "rev-list", "--left-right", "--count", base+"...HEAD")
	if err != nil {
		return 0, 0, err
	}

	// --left-right --count <base>...HEAD prints "leftCount rightCount": left
	// is commits reachable from base but not HEAD (behind), right is commits
	// reachable from HEAD but not base (ahead).
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("git rev-list --left-right --count: unexpected output %q", output)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list --left-right --count: %w", err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("git rev-list --left-right --count: %w", err)
	}
	return ahead, behind, nil
}
