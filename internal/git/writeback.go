package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// commitTimeout bounds the stage+commit pass. The 5s defaultGitTimeout is fine
// for a small tree but too tight for a large staging pass; this follows the
// worktreeTimeout precedent of overriding for multi-file ops. See Action 1 of
// the write-back design (plans/design/026-git-writeback-design.md).
const commitTimeout = 15 * time.Second

// ErrEmptyCommitMessage is returned by CommitAll when the message is blank.
// The UI must supply a non-empty message; commit-all never invents one.
var ErrEmptyCommitMessage = errors.New("commit message cannot be empty")

// CommitAll stages every change in workspaceRoot — tracked, untracked, and
// deletions via `git add -A` — and commits them with message via
// `git commit -m <message>`. Both git invocations run through the hardened
// RunGitCtx, so they inherit filteredGitEnv, the hooks/fsmonitor neutralization
// (hardenedGitArgs), the context timeout, and structured *Error on failure.
//
// message is passed as the argv value of -m and is never shell-interpolated, so
// a message beginning with '-' cannot be reparsed as a git flag. An empty or
// whitespace-only message is rejected with ErrEmptyCommitMessage.
//
// This is commit-only by design: no merge, push, force, amend, autostash, or
// base-branch checkout. The commit lands on whatever branch workspaceRoot has
// checked out. A clean tree is not special-cased here — `git commit` exits
// non-zero on an empty index and that structured error is returned; callers
// pre-check StatusResult.Clean before invoking to avoid the raw git error.
func CommitAll(ctx context.Context, workspaceRoot, message string) error {
	if strings.TrimSpace(message) == "" {
		return ErrEmptyCommitMessage
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()

	if _, err := RunGitCtx(ctx, workspaceRoot, "add", "-A"); err != nil {
		return fmt.Errorf("staging changes: %w", err)
	}
	if _, err := RunGitCtx(ctx, workspaceRoot, "commit", "-m", message); err != nil {
		return fmt.Errorf("committing changes: %w", err)
	}
	return nil
}
