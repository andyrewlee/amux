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
// precondition/conflict queries are fast.
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
// with `git merge --no-ff`.
//
// branch names a local branch. The exact refs/heads/<branch> ref is resolved
// first and the merge runs on the verified object ID, because Git's revision
// resolution lets refs/tags/<branch> shadow the short name — an unqualified
// operand could merge (or report "Already up to date" on) a tag that was never
// the workspace branch.
//
// It deliberately does not check out the base branch, fetch, rebase, squash,
// autostash, or push: the merge lands on the caller's current HEAD, and the
// caller is responsible for having verified that HEAD is the intended base
// (see CheckedOutBranch/LocalBaseBranch). --no-ff always records an explicit merge
// commit so the branch topology stays auditable, and `--` terminates options
// before the operand so it cannot be reparsed as a flag.
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

	// Resolve the exact local branch under the merge timeout budget. Merging
	// the verified object ID removes every ambiguity a short or full-looking
	// name could carry; a name with no local branch fails here, before git
	// can start a merge.
	localRef := "refs/heads/" + branch
	out, err := RunGitCtx(mergeCtx, repoPath, "show-ref", "--verify", "--hash", "--", localRef)
	if err != nil {
		return fmt.Errorf("merging %s: resolving local branch: %w", branch, err)
	}
	oid := strings.TrimSpace(out)
	if oid == "" || strings.ContainsAny(oid, "\r\n") {
		return fmt.Errorf("merging %s: resolving local branch: unexpected output %q", branch, out)
	}

	// A repo-configured merge driver (merge.<name>.driver) runs its command
	// on any conflicting file whose attributes name it — arbitrary code
	// execution under the user's identity during a routine UI merge, the
	// same threat class the hook/fsmonitor hardening exists to close. Refuse
	// when a configured driver is actually attributed in the tree; an
	// unconfigured or unattributed driver name can never fire, so those
	// merges proceed. The AMUX_ALLOW_GIT_HOOKS=1 opt-out stands the guard
	// down along with the rest of the hardening — opting into repo hooks is
	// opting into repo trust.
	if !allowRepoGitHooks {
		if live, err := liveMergeDrivers(mergeCtx, repoPath); err != nil {
			return fmt.Errorf("merging %s: checking merge drivers: %w", branch, err)
		} else if len(live) > 0 {
			return fmt.Errorf("merging %s: refusing: repo-configured merge driver %q is attributed to %s and would execute during the merge — merge manually after reviewing .git/config and .gitattributes", branch, live[0].driver, live[0].path)
		}
	}

	if _, err := RunGitCtx(mergeCtx, repoPath, "merge", "--no-ff", "--", oid); err != nil {
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

// mergeDriverAttribution pairs a configured merge driver with a tracked path
// whose attributes select it — a live repo-delivered exec binding.
type mergeDriverAttribution struct {
	driver string
	path   string
}

// liveMergeDrivers returns every configured merge driver (<name> from
// merge.<name>.driver) that at least one tracked path in the target tree
// attributes via `merge=<name>` — the pairs a merge could actually execute.
// Two enumerations bound the answer exactly: `git config --get-regexp` reads
// the merged config view (local .git/config plus inherited scopes — the
// repo-deliverable surface), and `git check-attr` applies the real attribute
// stack (.gitattributes at every tree level plus info/attributes), so
// unattributed driver names and unconfigured merge= values are correctly
// ignored.
func liveMergeDrivers(ctx context.Context, repoPath string) ([]mergeDriverAttribution, error) {
	out, err := RunGitAllowFailureCtx(ctx, repoPath, "config", "--get-regexp", `^merge\..*\.driver$`)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		key, _, _ := strings.Cut(line, " ")
		name, ok := strings.CutPrefix(key, "merge.")
		if !ok {
			continue
		}
		if name, ok = strings.CutSuffix(name, ".driver"); ok && name != "" {
			configured[name] = struct{}{}
		}
	}
	if len(configured) == 0 {
		return nil, nil
	}

	pathsRaw, err := RunGitRawCtx(ctx, repoPath, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(pathsRaw), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	var live []mergeDriverAttribution
	const checkAttrBatch = 200
	for i := 0; i < len(paths); i += checkAttrBatch {
		end := min(i+checkAttrBatch, len(paths))
		args := append([]string{"check-attr", "-z", "merge", "--"}, paths[i:end]...)
		raw, err := RunGitRawCtx(ctx, repoPath, args...)
		if err != nil {
			return nil, err
		}
		// -z output is path\0merge\0value\0 triples.
		fields := strings.Split(string(raw), "\x00")
		for j := 0; j+2 < len(fields); j += 3 {
			if _, ok := configured[fields[j+2]]; ok {
				live = append(live, mergeDriverAttribution{driver: fields[j+2], path: fields[j]})
			}
		}
	}
	return live, nil
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
// -z output is NUL-terminated, so paths are raw bytes — no C-quoting — and
// whitespace/newline-containing names survive intact.
func conflictedFiles(ctx context.Context, repoPath string) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, mergeInspectTimeout)
	defer cancel()

	out, err := RunGitRawCtx(ctx, repoPath, "diff", "--name-only", "--diff-filter=U", "-z", "--no-ext-diff", "--no-textconv")
	if err != nil {
		return nil, false
	}
	var files []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files = append(files, path)
		}
	}
	return files, len(files) > 0
}
