package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Timeouts for the merge write path. The 5s defaultGitTimeout is sized for
// single-file reads; a merge can touch the whole tree and invoke merge drivers,
// so it gets the worktreeTimeout-style override. Abort and the read-only
// precondition/conflict queries are fast. See Action 2 of the write-back design
// (plans/design/026-git-writeback-design.md).
const (
	mergeTimeout        = 30 * time.Second
	mergeAbortTimeout   = 10 * time.Second
	mergeInspectTimeout = 10 * time.Second
)

// ErrMergeConflict is returned by MergeWorkspaceBranch when git stopped with
// conflicts. It is a distinct sentinel because a conflict is an expected,
// recoverable outcome with its own UI (list the files, offer Abort), not a
// failure to report as raw stderr.
var ErrMergeConflict = errors.New("merge stopped with conflicts")

// MergeConflictError carries the conflicted paths alongside the sentinel so the
// UI can list exactly what needs resolving without re-querying git.
type MergeConflictError struct {
	Branch string
	Files  []string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("merging %s: %d conflicted file(s)", e.Branch, len(e.Files))
}

func (e *MergeConflictError) Unwrap() error { return ErrMergeConflict }

// CheckedOutBranch returns the branch checked out in repoPath, or an error when
// HEAD is detached.
//
// It is deliberately not GetCurrentBranch: that uses `rev-parse --abbrev-ref`,
// which reports the literal string "HEAD" for a detached HEAD instead of
// failing. For the merge precondition that is the wrong shape — "which branch
// will this merge land on?" has no answer when HEAD is detached, and
// `symbolic-ref` says so by exiting non-zero rather than returning a name that
// is not a branch.
func CheckedOutBranch(repoPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeInspectTimeout)
	defer cancel()
	out, err := RunGitCtx(ctx, repoPath, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// LocalBaseBranch resolves a stored base ref to the local branch a merge should
// land on: "origin/main" becomes "main", while "main" is unchanged.
//
// It asks the repository rather than splitting on the first "/", because
// Workspace.Base is not always remote-qualified. GetBaseBranch prefers a local
// branch when one exists, so a repo whose default branch is "release/2.0" stores
// exactly that — and blindly stripping the leading segment would turn it into
// "2.0" and make every merge refuse with a confusing mismatch.
//
// The question it actually answers is "which local branch does this ref name?",
// resolved in order:
//
//  1. refs/heads/<base> exists — the ref is already a local branch, use it.
//     This is what saves "release/2.0" from being mangled.
//  2. <base> has a leading segment and refs/heads/<rest> exists — the segment
//     was a remote qualifier, so "origin/main" resolves to "main". Testing the
//     stripped name directly (rather than checking that "origin" is a
//     configured remote) also handles metadata that outlived its remote.
//  3. Otherwise the ref is returned unchanged, so the caller's precondition
//     refuses with a clear mismatch rather than guessing.
func LocalBaseBranch(repoPath, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if localBranchExists(repoPath, base) {
		return base
	}
	if _, rest, found := strings.Cut(base, "/"); found && rest != "" {
		if localBranchExists(repoPath, rest) {
			return rest
		}
	}
	return base
}

// localBranchExists reports whether refs/heads/<branch> resolves in repoPath.
func localBranchExists(repoPath, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), mergeInspectTimeout)
	defer cancel()
	_, err := RunGitCtx(ctx, repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// MergeWorkspaceBranch merges branch into whatever is checked out in repoPath
// with `git merge --no-ff -- <branch>`.
//
// It deliberately does not check out the base branch, fetch, rebase, squash,
// autostash, or push: the merge lands on the caller's current HEAD, and the
// caller is responsible for having verified that HEAD is the intended base
// (see CheckedOutBranch/LocalBaseBranch). --no-ff always records an explicit merge
// commit so the branch topology stays auditable, and `--` terminates options
// before the branch name so a ref cannot be reparsed as a flag.
//
// A conflicting merge returns a *MergeConflictError listing the conflicted
// paths, leaving the merge in progress for the user to resolve or AbortMerge.
// Every other failure returns the structured *Error from RunGitCtx.
func MergeWorkspaceBranch(ctx context.Context, repoPath, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return errors.New("merge: branch is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mergeCtx, cancel := context.WithTimeout(ctx, mergeTimeout)
	defer cancel()

	if _, err := RunGitCtx(mergeCtx, repoPath, "merge", "--no-ff", "--", branch); err != nil {
		// Distinguish "stopped part-way, with state to clean up" from "could not
		// start at all". The signal is MERGE_HEAD rather than the presence of
		// unmerged files, because those are not the same set: a merge killed by
		// the timeout above can leave MERGE_HEAD with nothing yet marked
		// unmerged, and that repository still needs the abort the conflict
		// dialog offers. A merge that never began (unknown ref, dirty tree)
		// leaves no MERGE_HEAD and is a plain error.
		//
		// The inspect queries deliberately use the caller's ctx, not mergeCtx,
		// which is already expired on exactly the timeout path that needs them.
		if mergeInProgress(ctx, repoPath) {
			files, _ := conflictedFiles(ctx, repoPath)
			return &MergeConflictError{Branch: branch, Files: files}
		}
		return fmt.Errorf("merging %s: %w", branch, err)
	}
	return nil
}

// mergeInProgress reports whether repoPath has a merge waiting to be completed
// or aborted, i.e. whether MERGE_HEAD resolves.
func mergeInProgress(ctx context.Context, repoPath string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mergeInspectTimeout)
	defer cancel()

	_, err := RunGitCtx(ctx, repoPath, "rev-parse", "--verify", "--quiet", "MERGE_HEAD")
	return err == nil
}

// AbortMerge abandons an in-progress merge in repoPath, restoring the
// pre-merge state via `git merge --abort`. It takes no user input, so no `--`
// terminator is needed.
func AbortMerge(ctx context.Context, repoPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mergeAbortTimeout)
	defer cancel()

	if _, err := RunGitCtx(ctx, repoPath, "merge", "--abort"); err != nil {
		return fmt.Errorf("aborting merge: %w", err)
	}
	return nil
}

// conflictedFiles lists the unmerged paths in repoPath, for display. The bool
// reports whether any were found; callers use mergeInProgress to decide whether
// a merge needs cleaning up, since an interrupted merge can have MERGE_HEAD
// without having marked anything unmerged yet.
//
// Paths are only stripped of the line terminator, never trimmed: a filename may
// legitimately begin or end with a space, and git already quotes names
// containing control characters, so a line is the path exactly as git reports it.
func conflictedFiles(ctx context.Context, repoPath string) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mergeInspectTimeout)
	defer cancel()

	out, err := RunGitCtx(ctx, repoPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, false
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if path := strings.TrimSuffix(line, "\r"); path != "" {
			files = append(files, path)
		}
	}
	return files, len(files) > 0
}
