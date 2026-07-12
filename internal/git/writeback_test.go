package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitAllCreatesCommit stages a dirty tree and commits it, asserting the
// commit lands with the given message and the working tree ends clean. It runs
// against a real temp repo built by testutil.InitRepo.
func TestCommitAllCreatesCommit(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	// Dirty the tree: a tracked modification plus a brand-new untracked file, so
	// `add -A` must pick up both.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("brand new\n"), 0o600); err != nil {
		t.Fatalf("write new.txt: %v", err)
	}

	const msg = "wire up commit-all"
	if err := CommitAll(context.Background(), root, msg); err != nil {
		t.Fatalf("CommitAll: unexpected error: %v", err)
	}

	// The newest commit carries the message.
	if got := runGit(t, root, "log", "-1", "--pretty=%s"); got != msg {
		t.Fatalf("commit subject = %q, want %q", got, msg)
	}
	// Both files are now committed and the tree is clean.
	if got := runGit(t, root, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean after CommitAll: %q", got)
	}
	// The untracked file was staged and committed, not left behind.
	tracked := runGit(t, root, "ls-files", "new.txt")
	if strings.TrimSpace(tracked) != "new.txt" {
		t.Fatalf("new.txt not tracked after CommitAll: ls-files = %q", tracked)
	}
}

// TestCommitAllEmptyMessageRejected asserts a blank message is refused before
// any git runs, so no commit is created.
func TestCommitAllEmptyMessageRejected(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}

	before := runGit(t, root, "rev-parse", "HEAD")

	for _, msg := range []string{"", "   ", "\t\n"} {
		err := CommitAll(context.Background(), root, msg)
		if !errors.Is(err, ErrEmptyCommitMessage) {
			t.Fatalf("CommitAll(%q): got %v, want ErrEmptyCommitMessage", msg, err)
		}
	}

	// HEAD is unchanged: rejection happened before any git invocation, and the
	// dirty change is still uncommitted.
	if after := runGit(t, root, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved after rejected commit: %q -> %q", before, after)
	}
	if got := runGit(t, root, "status", "--porcelain"); got == "" {
		t.Fatalf("expected the tree to still be dirty after a rejected commit")
	}
}

// TestCommitAllCleanTreeReturnsError documents the clean-tree contract: CommitAll
// does not special-case an empty index, so `git commit` exits non-zero and the
// structured git error surfaces. The UI pre-checks StatusResult.Clean to avoid
// reaching this path; this test pins the fallback behavior.
func TestCommitAllCleanTreeReturnsError(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t) // initRepo leaves a clean tree with one commit.

	before := runGit(t, root, "rev-parse", "HEAD")

	err := CommitAll(context.Background(), root, "nothing to do")
	if err == nil {
		t.Fatalf("CommitAll on a clean tree: expected an error, got nil")
	}
	// Empty-message sentinel must not be what a clean tree returns.
	if errors.Is(err, ErrEmptyCommitMessage) {
		t.Fatalf("clean tree returned ErrEmptyCommitMessage, want a git error")
	}

	if after := runGit(t, root, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved on a clean-tree commit: %q -> %q", before, after)
	}
}

// TestCommitAllMessageNotShellInterpolated commits a message containing shell
// metacharacters and a leading dash, then reads it back verbatim. Because the
// message is the argv value of -m (never a shell string and never parsed as a
// flag), it round-trips exactly.
func TestCommitAllMessageNotShellInterpolated(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}

	const msg = "--not-a-flag $(touch pwned) `id` && echo hi"
	if err := CommitAll(context.Background(), root, msg); err != nil {
		t.Fatalf("CommitAll: unexpected error: %v", err)
	}

	if got := runGit(t, root, "log", "-1", "--pretty=%s"); got != msg {
		t.Fatalf("commit subject = %q, want verbatim %q", got, msg)
	}
	// The $(touch pwned) substring must not have run.
	if _, err := os.Stat(filepath.Join(root, "pwned")); !os.IsNotExist(err) {
		t.Fatalf("shell metacharacters were interpreted: pwned exists (err=%v)", err)
	}
}
