package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultGitTimeout = 15 * time.Second

// RunGit executes a git command in the specified directory.
func RunGit(dir string, args ...string) (string, error) {
	return RunGitCtx(context.Background(), dir, args...)
}

// RunGitCtx executes a git command in the specified directory with context.
func RunGitCtx(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = filteredGitEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		// Include stderr in error for debugging
		if stderr.Len() > 0 {
			return "", &GitError{
				Command: strings.Join(args, " "),
				Stderr:  stderr.String(),
				Err:     err,
			}
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GitError wraps git command errors with additional context
type GitError struct {
	Command string
	Stderr  string
	Err     error
}

func (e *GitError) Error() string {
	return "git " + e.Command + ": " + e.Stderr
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// IsGitRepository checks if the given path is a git repository
func IsGitRepository(path string) bool {
	_, err := RunGitCtx(context.Background(), path, "rev-parse", "--git-dir")
	return err == nil
}

// GetRepoRoot returns the root directory of the git repository
func GetRepoRoot(path string) (string, error) {
	return RunGitCtx(context.Background(), path, "rev-parse", "--show-toplevel")
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(path string) (string, error) {
	return RunGitCtx(context.Background(), path, "rev-parse", "--abbrev-ref", "HEAD")
}

// GetRemoteURL returns the URL of the specified remote
func GetRemoteURL(path, remote string) (string, error) {
	return RunGitCtx(context.Background(), path, "remote", "get-url", remote)
}

// RunGitAllowFailure executes git and returns stdout even if exit code is non-zero.
// Use for commands like `git diff --no-index` which return 1 when differences exist.
func RunGitAllowFailure(dir string, args ...string) (string, error) {
	return RunGitAllowFailureCtx(context.Background(), dir, args...)
}

// RunGitAllowFailureCtx executes git and returns stdout even if exit code is non-zero.
// Use for commands like `git diff --no-index` which return 1 when differences exist.
func RunGitAllowFailureCtx(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = filteredGitEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run() // Ignore exit code - some commands return 1 on success
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}

	// Only return error if there's actual stderr output indicating a problem
	// and no stdout (which would indicate the command worked but returned non-zero)
	if stderr.Len() > 0 && stdout.Len() == 0 {
		return "", &GitError{
			Command: strings.Join(args, " "),
			Stderr:  stderr.String(),
			Err:     err,
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RunGitRaw executes a git command and returns raw bytes without trimming.
// Use this for commands with -z output that use NUL separators.
func RunGitRaw(dir string, args ...string) ([]byte, error) {
	return RunGitRawCtx(context.Background(), dir, args...)
}

// RunGitRawCtx executes a git command and returns raw bytes without trimming.
// Use this for commands with -z output that use NUL separators.
func RunGitRawCtx(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = filteredGitEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		if stderr.Len() > 0 {
			return nil, &GitError{
				Command: strings.Join(args, " "),
				Stderr:  stderr.String(),
				Err:     err,
			}
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

func ensureGitTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultGitTimeout)
}

func filteredGitEnv() []string {
	// Filter out GIT_ environment variables to ensure we run against the target repo
	// and ignore any parent git process environment (e.g. when running in hooks)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	return env
}
