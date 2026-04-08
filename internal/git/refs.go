package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	refResolveTimeout = 10 * time.Second
	rebaseTimeout     = 60 * time.Second
	pushTimeout       = 90 * time.Second
)

// ResolveRefCommit resolves a ref-like input to a commit SHA.
func ResolveRefCommit(repoPath, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}
	ctx, cancel := context.WithTimeout(context.Background(), refResolveTimeout)
	defer cancel()
	return RunGitCtx(ctx, repoPath, "rev-parse", "--verify", ref+"^{commit}")
}

// MergeBase returns the merge base commit between two refs.
func MergeBase(repoPath, left, right string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), refResolveTimeout)
	defer cancel()
	return RunGitCtx(ctx, repoPath, "merge-base", strings.TrimSpace(left), strings.TrimSpace(right))
}

// RebaseCurrentBranchOnto rebases the current branch in repoPath onto newBase.
func RebaseCurrentBranchOnto(repoPath, newBase, oldBase string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rebaseTimeout)
	defer cancel()
	_, err := RunGitCtx(ctx, repoPath, "rebase", "--onto", strings.TrimSpace(newBase), strings.TrimSpace(oldBase))
	return err
}

// AbortRebaseIfInProgress aborts an in-progress rebase. It is a no-op when the
// repository is not currently rebasing.
func AbortRebaseIfInProgress(repoPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rebaseTimeout)
	defer cancel()
	_, err := RunGitCtx(ctx, repoPath, "rebase", "--abort")
	if err == nil {
		return nil
	}
	var gitErr *GitError
	if errors.As(err, &gitErr) {
		stderr := strings.ToLower(strings.TrimSpace(gitErr.Stderr))
		if strings.Contains(stderr, "no rebase in progress") ||
			strings.Contains(stderr, "no rebase-apply") ||
			strings.Contains(stderr, "no rebase to abort") {
			return nil
		}
	}
	return err
}

// ResetCurrentBranchHard resets the checked-out branch to targetCommit.
func ResetCurrentBranchHard(repoPath, targetCommit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rebaseTimeout)
	defer cancel()
	_, err := RunGitCtx(ctx, repoPath, "reset", "--hard", strings.TrimSpace(targetCommit))
	return err
}

// CountCommitsAhead returns how many commits head is ahead of base.
func CountCommitsAhead(repoPath, base, head string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), refResolveTimeout)
	defer cancel()
	out, err := RunGitCtx(ctx, repoPath, "rev-list", "--count", strings.TrimSpace(base)+".."+strings.TrimSpace(head))
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, err
	}
	return count, nil
}

// PushBranch pushes a local branch to a remote and sets upstream tracking.
func PushBranch(repoPath, remote, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	_, err := RunGitCtx(ctx, repoPath, "push", "-u", strings.TrimSpace(remote), strings.TrimSpace(branch))
	return err
}

// PushBranchForceWithLease pushes a local branch using force-with-lease, which
// is appropriate for rebased stacked branches.
func PushBranchForceWithLease(repoPath, remote, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	_, err := RunGitCtx(
		ctx,
		repoPath,
		"push",
		"--force-with-lease",
		"-u",
		strings.TrimSpace(remote),
		strings.TrimSpace(branch),
	)
	return err
}

// GetLastCommitSubject returns the subject of the current HEAD commit.
func GetLastCommitSubject(repoPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), refResolveTimeout)
	defer cancel()
	return RunGitCtx(ctx, repoPath, "log", "-1", "--pretty=%s")
}
