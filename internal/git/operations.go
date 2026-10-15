package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
)

const defaultGitTimeout = 5 * time.Second

// gitOutputDrainDelay bounds how long exec.Cmd.Wait waits for inherited
// stdout/stderr pipes once the git leader is gone. A spawned descendant that
// keeps a pipe open must not stretch a caller's deadline into an indefinite
// hang.
const gitOutputDrainDelay = 500 * time.Millisecond

// noHooksPath is the core.hooksPath value used to disable repo hooks. It must
// be a non-empty path that can never hold a hook and can never be written to.
//
// An empty value ("core.hooksPath=") also stops git from finding hooks, but it
// breaks third-party tools that read core.hooksPath themselves: git-lfs
// re-installs its hooks on every filter invocation (`git lfs filter-process`
// and `git lfs smudge`, both driven by a repo's .gitattributes during
// checkout), resolves the empty value to "" (a set-but-empty config value,
// which it treats as a real path rather than as unset), and so
// writes post-checkout/post-commit/post-merge/pre-push relative to the process
// working directory. In an lfs-using repo that dropped four untracked hook
// files into the root of every workspace amux created. Pointing at /dev/null
// makes the same install attempt fail harmlessly and leaves the tree clean.
// amux is Linux/macOS-only, so /dev/null is always present.
const noHooksPath = "/dev/null"

// hardenedGitArgs prepends config flags that neutralize repo-controlled code
// execution on five vectors: post-checkout/pre-* hooks
// (core.hooksPath), core.fsmonitor, and gpg-signing on commit
// (commit.gpgsign), merge (merge.verifySignatures), and tag
// (tag.gpgSign) — each signing key would otherwise run a repo-configured
// gpg.program. amux runs git in repositories it does not control, and a
// repo delivered with a populated .git (hooks, core.fsmonitor, or
// signing config in .git/config plus signed commits) must not auto-run its
// code just because amux touched it. It is a no-op when the user opted in via
// AMUX_ALLOW_GIT_HOOKS=1 — opting into repo hooks is opting into repo trust,
// so signing is restored too.
//
// Merge drivers (merge.<name>.driver) are repo-configured commands git runs
// on conflicting attributed files — they cannot be flag-neutralized because
// driver names are arbitrary, so MergeWorkspaceBranch enumerates configured
// drivers and refuses when the target tree attributes any of them (see
// merge.go). Filter drivers (filter.<name>.clean/smudge/process, repo
// .gitattributes clean/smudge/process named in-tree) get the same
// enumeration+refusal treatment at every mutation-capable call site —
// worktree add, add -A, merge (see filter_drivers.go); only repo-LOCAL
// config is enumerated, so the user's own global lfs install is unaffected.
// NOT neutralized: diff textconv/external-diff drivers named in-tree —
// diff/browse read paths pass --no-ext-diff/--no-textconv per call site
// instead (see diff.go).
func hardenedGitArgs(args []string) []string {
	if ops.allowRepoGitHooks {
		return args
	}
	// core.hooksPath points git's hook lookup at noHooksPath so no repo hook is
	// found or run; core.fsmonitor=false disables any repo-configured fsmonitor
	// program; commit.gpgsign=false stops `git commit` from invoking a
	// repo-configured gpg.program, and merge.verifySignatures/tag.gpgSign close
	// the same gpg.program channel on `git merge`/`git tag` (a bundled repo can
	// ship both the config keys and the signed commits that trigger them).
	prefix := []string{
		"-c", "core.hooksPath=" + noHooksPath,
		"-c", "core.fsmonitor=false",
		"-c", "commit.gpgsign=false",
		"-c", "merge.verifySignatures=false",
		"-c", "tag.gpgSign=false",
	}
	return append(prefix, args...)
}

// newGitCmd builds a hardened git command rooted at dir. The child runs in
// its own process group so cancellation can reach descendants, not just the
// leader (a no-op on Windows), and WaitDelay keeps an orphaned pipe writer
// from blocking Wait past the drain allowance.
func newGitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", hardenedGitArgs(args)...)
	cmd.Dir = dir
	cmd.Env = filteredGitEnv()
	process.SetProcessGroup(cmd)
	cmd.WaitDelay = gitOutputDrainDelay
	return cmd
}

// RunGitCtx executes a git command in the specified directory with context.
func RunGitCtx(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	args, err := neutralizeGitArgs(ctx, dir, args)
	if err != nil {
		return "", err
	}
	cmd := newGitCmd(dir, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	killedByContext, err := runGitCommand(ctx, cmd)
	if err != nil {
		if ctxErr := gitCommandContextErrorWithKill(ctx, err, args, killedByContext); ctxErr != nil {
			return "", ctxErr
		}
		// Every non-context failure is structured so callers can classify by
		// exit code and stderr.
		return "", newGitError(args, stderr.String(), err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// Error wraps git command errors with structured context: the exact argv,
// the process exit code, and captured stderr. Callers classify failures by
// matching ExitCode/Stderr through errors.As instead of parsing the prose of
// Error().
type Error struct {
	Command string   // joined args, for display
	Args    []string // exact argv passed to git
	// ExitCode is the git process exit code; -1 when the process did not run
	// or did not exit normally.
	ExitCode int
	Stderr   string
	Err      error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return "git " + e.Command + ": " + e.Stderr
	}
	if e.Err != nil {
		return "git " + e.Command + ": " + e.Err.Error()
	}
	return "git " + e.Command
}

func (e *Error) Unwrap() error {
	return e.Err
}

// newGitError builds a structured Error for a failed git invocation.
func newGitError(args []string, stderr string, err error) *Error {
	exitCode := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	return &Error{
		Command:  strings.Join(args, " "),
		Args:     append([]string(nil), args...),
		ExitCode: exitCode,
		Stderr:   stderr,
		Err:      err,
	}
}

// IsGitRepository checks if the given path is a git repository
func IsGitRepository(path string) bool {
	_, err := RunGitCtx(context.Background(), path, "rev-parse", "--git-dir")
	return err == nil
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(path string) (string, error) {
	return RunGitCtx(context.Background(), path, "rev-parse", "--abbrev-ref", "HEAD")
}

// RunGitAllowFailureCtx executes git and returns stdout even if the exit
// code is non-zero — process exit statuses are the tolerated part. Use for
// commands like `git diff --no-index` which return 1 when differences
// exist. A failure to launch or reap git is never tolerated: it returns a
// structured error so a missing executable or bad directory cannot masquerade
// as a successful empty result.
func RunGitAllowFailureCtx(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	args, err := neutralizeGitArgs(ctx, dir, args)
	if err != nil {
		return "", err
	}
	cmd := newGitCmd(dir, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	killedByContext, err := runGitCommand(ctx, cmd) // Tolerate exit status — some commands return 1 on success
	if err != nil {
		if ctxErr := gitAllowFailureCommandContextErrorWithKill(ctx, err, args, stdout.Len(), killedByContext); ctxErr != nil {
			return "", ctxErr
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// Not a process exit — git never ran or the drain/wait
			// infrastructure failed. Returning it prevents a launch
			// failure from looking like a successful empty result.
			return "", newGitError(args, stderr.String(), err)
		}
	}

	// Only return error when git actually failed (tolerated ExitError) AND
	// there's stderr output indicating a problem and no stdout. A clean exit
	// (err == nil) with stderr chatter — unreadable ~/.gitconfig, a stale
	// include.path, safe.directory warnings — is success, not failure: git
	// routinely warns on stderr while exiting 0.
	if err != nil && stderr.Len() > 0 && stdout.Len() == 0 {
		return "", newGitError(args, stderr.String(), err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RunGitRawCtx executes a git command and returns raw bytes without trimming.
// Use this for commands with -z output that use NUL separators.
func RunGitRawCtx(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return RunGitRawStdinCtx(ctx, dir, nil, args...)
}

// RunGitRawStdinCtx executes a git command with stdin fed from the given
// bytes and returns raw stdout without trimming — for commands like
// `check-attr --stdin` that stream bulk input over the pipe.
func RunGitRawStdinCtx(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := ensureGitTimeout(ctx)
	defer cancel()

	args, err := neutralizeGitArgs(ctx, dir, args)
	if err != nil {
		return nil, err
	}
	cmd := newGitCmd(dir, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	killedByContext, err := runGitCommand(ctx, cmd)
	if err != nil {
		if ctxErr := gitCommandContextErrorWithKill(ctx, err, args, killedByContext); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newGitError(args, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

func gitCommandContextErrorWithKill(ctx context.Context, runErr error, args []string, killedByContext bool) error {
	return gitCommandContextErrorWithKillForGOOS(ctx, runtime.GOOS, runErr, args, killedByContext)
}

func gitAllowFailureCommandContextErrorWithKill(
	ctx context.Context,
	runErr error,
	args []string,
	stdoutLen int,
	killedByContext bool,
) error {
	return gitAllowFailureCommandContextErrorWithKillForGOOS(
		ctx,
		runtime.GOOS,
		runErr,
		args,
		stdoutLen,
		killedByContext,
	)
}

func gitAllowFailureCommandContextErrorWithKillForGOOS(
	ctx context.Context,
	goos string,
	runErr error,
	args []string,
	stdoutLen int,
	killedByContext bool,
) error {
	if stdoutLen > 0 && isAmbiguousWindowsAllowFailureCanceledExit(ctx, goos, runErr, killedByContext) {
		return nil
	}
	return gitCommandContextErrorWithKillForGOOS(ctx, goos, runErr, args, killedByContext)
}

func gitCommandContextErrorWithKillForGOOS(
	ctx context.Context,
	goos string,
	runErr error,
	args []string,
	killedByContext bool,
) error {
	if ctx == nil || runErr == nil {
		return nil
	}
	if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), runErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return nil
	}
	if err := ctx.Err(); err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		if killedByContext || isCommandContextTerminationExitCode(exitErr.ExitCode()) {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

func isCommandContextTerminationExitCode(exitCode int) bool {
	return exitCode == -1
}

func isAmbiguousWindowsAllowFailureCanceledExit(
	ctx context.Context,
	goos string,
	runErr error,
	killedByContext bool,
) bool {
	if goos != "windows" || ctx == nil || runErr == nil {
		return false
	}
	if err := ctx.Err(); err == nil || !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var exitErr *exec.ExitError
	return !killedByContext && errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1
}

func runGitCommand(ctx context.Context, cmd *exec.Cmd) (bool, error) {
	if ctx == nil {
		return false, cmd.Run()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	var killedByContext atomic.Bool
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	select {
	case err := <-waitCh:
		if ops.runGitCommandAfterWaitHook != nil {
			ops.runGitCommandAfterWaitHook()
		}
		return killedByContext.Load(), err
	case <-ctx.Done():
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err == nil {
				killedByContext.Store(true)
			}
			// The leader's exit does not reap spawned descendants (hooks,
			// credential helpers, alias shells) that may hold worktree
			// locks or the output pipes. The group outlives an exited
			// leader, so signal it unconditionally.
			if err := process.KillProcessGroup(cmd.Process.Pid, process.KillOptions{}); err != nil {
				logging.Debug("git cancel: process-group termination failed for pid %d: %v", cmd.Process.Pid, err)
			}
		}
		err := <-waitCh
		if ops.runGitCommandAfterWaitHook != nil {
			ops.runGitCommandAfterWaitHook()
		}
		if err == nil && killedByContext.Load() {
			return true, ctx.Err()
		}
		return killedByContext.Load(), err
	}
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
		if !isGitProcessEnv(e) {
			env = append(env, e)
		}
	}
	return env
}

// isGitProcessEnv reports whether an env entry is git-process state that a
// child git must not inherit. Two families are stripped:
//
//   - Repository-discovery vars (GIT_DIR and friends): a parent git process
//     (a hook, a `git -c` wrapper) sets them to point at ITS repo, which
//     would redirect every amux git call away from the target worktree.
//   - Config-injection vars (GIT_CONFIG_PARAMETERS, GIT_CONFIG_COUNT +
//     GIT_CONFIG_KEY_*/GIT_CONFIG_VALUE_*, GIT_CONFIG, GIT_CONFIG_GLOBAL,
//     GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM): these carry the same config
//     surface hardenedGitArgs locks down via -c flags — an env-carried
//     merge.verifySignatures or core.sshCommand would bypass the argv
//     hardening entirely. The namespace/quarantine/template/attr-source/
//     external-diff/editor vars are stripped for the same reason: they only
//     exist when a parent git process is mid-operation.
//
// Deliberately KEPT (documented, do not strip without a migration):
// GIT_SSH_COMMAND, GIT_SSH, GIT_ASKPASS (user SSH/agent config for remotes),
// GIT_AUTHOR_*/GIT_COMMITTER_*/GIT_*_DATE (commit identity — CommitAll sets
// no author itself, so stripping these would change attribution for users
// who rely on env identity), and GIT_OPTIONAL_LOCKS/GIT_INDEX_VERSION
// (legitimate user index tuning).
func isGitProcessEnv(kv string) bool {
	key, _, ok := strings.Cut(kv, "=")
	if !ok {
		key = kv
	}
	switch key {
	case "GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_CEILING_DIRECTORIES",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_GLOBAL",
		"GIT_CONFIG_SYSTEM",
		"GIT_CONFIG_NOSYSTEM",
		"GIT_NAMESPACE",
		"GIT_TEMPLATE_DIR",
		"GIT_QUARANTINE_PATH",
		"GIT_ATTR_SOURCE",
		"GIT_EXTERNAL_DIFF",
		"GIT_EDITOR",
		"GIT_PREFIX":
		return true
	default:
		return strings.HasPrefix(key, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(key, "GIT_CONFIG_VALUE_")
	}
}
