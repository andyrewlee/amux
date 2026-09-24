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
		_, err := RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
	}

	// Try remote tracking branches for common candidates
	for _, branch := range candidates {
		remote := "origin/" + branch
		_, err := RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", remote)
		if err == nil {
			return remote, nil
		}
	}

	// Try to get the default branch from remote
	output, err := RunGitCtx(context.Background(), repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main" or "refs/remotes/origin/feature/foo"
		branch := strings.TrimPrefix(output, "refs/remotes/origin/")
		// Verify the branch exists locally
		_, err := RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", branch)
		if err == nil {
			return branch, nil
		}
		// Try remote tracking branch for symbolic-ref result
		remote := "origin/" + branch
		_, err = RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", remote)
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

	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "-U3", mergeBase + "...HEAD", "--", path}
	ctx, cancel := context.WithTimeout(context.Background(), branchDiffTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), branchDiffTimeout)
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

	ctx, cancel := context.WithTimeout(context.Background(), branchDiffTimeout)
	defer cancel()
	output, err := RunGitRawCtx(ctx, repoPath, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--name-status", "-z", mergeBase+"...HEAD")
	if err != nil {
		return nil, err
	}

	return parseNameStatus(output), nil
}

// parseNameStatus parses `git diff --name-status -z` output into Changes. With
// -z, records are NUL-terminated and fields are NUL-separated: "M\0path\0" for
// single-path statuses and "R100\0old\0new\0" for renames/copies. Paths are raw
// bytes — no C-quoting — so non-ASCII and whitespace-containing names survive.
// The same status-code mapping as working-tree status parsing is reused.
func parseNameStatus(output []byte) []Change {
	if len(output) == 0 {
		return nil
	}
	tokens := strings.Split(string(output), "\x00")
	var changes []Change
	for i := 0; i < len(tokens); i++ {
		code := tokens[i]
		if code == "" {
			continue
		}
		change := Change{Kind: statusCodeToKind(code[0])}
		paths := 1
		if code[0] == 'R' || code[0] == 'C' {
			paths = 2
		}
		if i+paths >= len(tokens) {
			break // truncated final record
		}
		if paths == 2 {
			change.OldPath = tokens[i+1]
			change.Path = tokens[i+2]
		} else {
			change.Path = tokens[i+1]
		}
		i += paths
		if change.Path == "" {
			continue
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

	ctx, cancel := context.WithTimeout(context.Background(), branchDiffTimeout)
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
