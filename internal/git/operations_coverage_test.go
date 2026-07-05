package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestErrorUnwrap verifies that the structured git Error participates in the
// standard errors.Unwrap/errors.Is chain by exposing its wrapped cause.
func TestErrorUnwrap(t *testing.T) {
	sentinel := errors.New("boom")

	tests := []struct {
		name string
		err  *Error
		want error
	}{
		{
			name: "wraps underlying error",
			err:  &Error{Command: "status", Err: sentinel},
			want: sentinel,
		},
		{
			name: "nil underlying error",
			err:  &Error{Command: "status"},
			want: nil,
		},
		{
			name: "wraps exec exit error from newGitError",
			err:  newGitError([]string{"diff"}, "bad", &exec.ExitError{}),
			want: nil, // compared via errors.As below, not direct equality
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Unwrap()
			if tt.name == "wraps exec exit error from newGitError" {
				// The wrapped cause must be reachable through errors.As.
				var exitErr *exec.ExitError
				if !errors.As(tt.err, &exitErr) {
					t.Fatalf("errors.As() could not reach wrapped *exec.ExitError via Unwrap")
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("Unwrap() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestErrorUnwrapEnablesErrorsIs confirms Unwrap lets a sentinel cause be
// matched through an arbitrarily deep wrapping chain.
func TestErrorUnwrapEnablesErrorsIs(t *testing.T) {
	sentinel := errors.New("root cause")
	gitErr := &Error{Command: "fetch", Stderr: "network down", Err: sentinel}

	if !errors.Is(gitErr, sentinel) {
		t.Fatalf("errors.Is() should find the sentinel cause through Unwrap")
	}

	other := errors.New("unrelated")
	if errors.Is(gitErr, other) {
		t.Fatalf("errors.Is() must not match an unrelated error")
	}
}

func writeDiffFixtures(t *testing.T) (left, right, identicalA, identicalB string) {
	t.Helper()
	tmp := t.TempDir()
	left = filepath.Join(tmp, "left.txt")
	right = filepath.Join(tmp, "right.txt")
	identicalA = filepath.Join(tmp, "same_a.txt")
	identicalB = filepath.Join(tmp, "same_b.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}
	if err := os.WriteFile(identicalA, []byte("same\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(identicalA) error = %v", err)
	}
	if err := os.WriteFile(identicalB, []byte("same\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(identicalB) error = %v", err)
	}
	return left, right, identicalA, identicalB
}

// TestRunGitAllowFailureCtx exercises the core contract of
// RunGitAllowFailureCtx: a non-zero exit code that still produces stdout (e.g.
// `git diff --no-index` when files differ) is treated as success, while a
// genuine failure that only writes stderr surfaces a structured *Error.
func TestRunGitAllowFailureCtx(t *testing.T) {
	skipIfNoGit(t)

	left, right, identicalA, identicalB := writeDiffFixtures(t)

	tests := []struct {
		name        string
		dir         string
		args        []string
		wantErr     bool
		wantContain string // substring expected in returned stdout (when no error)
		wantEmpty   bool   // stdout should be empty
	}{
		{
			name:        "diff with differences returns stdout despite exit 1",
			dir:         filepath.Dir(left),
			args:        []string{"diff", "--no-index", "--no-color", "--", left, right},
			wantErr:     false,
			wantContain: "+right",
		},
		{
			name:      "diff of identical files returns empty output and no error",
			dir:       filepath.Dir(identicalA),
			args:      []string{"diff", "--no-index", "--no-color", "--", identicalA, identicalB},
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name:    "unknown subcommand surfaces structured error",
			dir:     filepath.Dir(left),
			args:    []string{"notarealsubcommand"},
			wantErr: true,
		},
		{
			// `git` with no args prints usage to stdout and the allow-failure
			// contract treats any stdout as success, so no error is returned.
			name:        "empty args returns usage text without error",
			dir:         filepath.Dir(left),
			args:        nil,
			wantErr:     false,
			wantContain: "usage: git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := RunGitAllowFailureCtx(context.Background(), tt.dir, tt.args...)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("RunGitAllowFailureCtx() expected error, got out=%q", out)
				}
				if out != "" {
					t.Fatalf("RunGitAllowFailureCtx() error path should return empty stdout, got %q", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunGitAllowFailureCtx() unexpected error = %v", err)
			}
			if tt.wantEmpty && out != "" {
				t.Fatalf("RunGitAllowFailureCtx() = %q, want empty", out)
			}
			if tt.wantContain != "" && !strings.Contains(out, tt.wantContain) {
				t.Fatalf("RunGitAllowFailureCtx() = %q, want substring %q", out, tt.wantContain)
			}
		})
	}
}

// TestRunGitAllowFailureCtxStructuredError checks that the genuine-failure path
// returns a *Error carrying the exact argv, stderr, and a non-zero exit code so
// callers can classify the failure.
func TestRunGitAllowFailureCtxStructuredError(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	_, err := RunGitAllowFailureCtx(context.Background(), repo, "notarealsubcommand")
	if err == nil {
		t.Fatalf("expected error for unknown subcommand")
	}

	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if gitErr.Command != "notarealsubcommand" {
		t.Fatalf("Error.Command = %q, want %q", gitErr.Command, "notarealsubcommand")
	}
	if len(gitErr.Args) != 1 || gitErr.Args[0] != "notarealsubcommand" {
		t.Fatalf("Error.Args = %v, want [notarealsubcommand]", gitErr.Args)
	}
	if gitErr.Stderr == "" {
		t.Fatalf("Error.Stderr should capture git's complaint, got empty")
	}
	if gitErr.ExitCode == 0 {
		t.Fatalf("Error.ExitCode = 0, want a non-zero git exit code")
	}
}

// TestRunGitAllowFailureCtxNonRepoMetadataCommand verifies that a failing
// metadata command (which writes only to stderr) in a non-repo directory is
// reported as an error rather than swallowed.
func TestRunGitAllowFailureCtxNonRepoMetadataCommand(t *testing.T) {
	skipIfNoGit(t)

	nonRepo := t.TempDir()
	_, err := RunGitAllowFailureCtx(context.Background(), nonRepo, "rev-parse", "--show-toplevel")
	if err == nil {
		t.Fatalf("expected error running rev-parse outside a repo")
	}
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
}

// TestRunGitAllowFailureCtxTimeout verifies the context-cancellation path:
// an already-expired deadline yields a wrapped context.DeadlineExceeded error
// that still names the command.
func TestRunGitAllowFailureCtxTimeout(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("shell sleep alias is unix-specific")
	}

	repo := initRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := RunGitAllowFailureCtx(ctx, repo, "-c", "alias.sleep=!sleep 1", "sleep")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
	if !strings.Contains(err.Error(), "sleep") {
		t.Fatalf("expected error to include command context, got %v", err)
	}
}

// TestRunGitAllowFailureCtxNilContext confirms a nil context is tolerated: it
// defaults to a background context with the package timeout and still runs.
func TestRunGitAllowFailureCtxNilContext(t *testing.T) {
	skipIfNoGit(t)

	left, right, _, _ := writeDiffFixtures(t)
	var nilCtx context.Context // intentionally nil to exercise the defaulting path.
	out, err := RunGitAllowFailureCtx(nilCtx, filepath.Dir(left), "diff", "--no-index", "--no-color", "--", left, right)
	if err != nil {
		t.Fatalf("RunGitAllowFailureCtx(nil ctx) unexpected error = %v", err)
	}
	if !strings.Contains(out, "+right") {
		t.Fatalf("RunGitAllowFailureCtx(nil ctx) = %q, want diff output", out)
	}
}

// TestGitAllowFailureCommandContextErrorWithKill exercises the runtime.GOOS
// dispatcher wrapper directly, covering its nil-input guards and its delegation
// to the GOOS-specific implementation on the host platform.
func TestGitAllowFailureCommandContextErrorWithKill(t *testing.T) {
	t.Run("nil context returns nil", func(t *testing.T) {
		var nilCtx context.Context // intentionally nil to exercise the guard.
		if err := gitAllowFailureCommandContextErrorWithKill(nilCtx, errors.New("x"), []string{"status"}, 0, false); err != nil {
			t.Fatalf("expected nil for nil context, got %v", err)
		}
	})

	t.Run("nil run error returns nil", func(t *testing.T) {
		if err := gitAllowFailureCommandContextErrorWithKill(
			context.Background(), nil, []string{"status"}, 0, false,
		); err != nil {
			t.Fatalf("expected nil for nil run error, got %v", err)
		}
	})

	t.Run("live context with deadline error wraps and names command", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := gitAllowFailureCommandContextErrorWithKill(
			ctx, context.DeadlineExceeded, []string{"diff", "--no-index"}, 0, false,
		)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected wrapped deadline error, got %v", err)
		}
		if !strings.Contains(err.Error(), "git diff --no-index") {
			t.Fatalf("expected command context in error, got %v", err)
		}
	})

	t.Run("plain non-exit error returns nil on host", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// A non-*exec.ExitError, non-context error is not a context termination,
		// so the dispatcher must report nil regardless of stdout length.
		if err := gitAllowFailureCommandContextErrorWithKill(
			ctx, errors.New("plain failure"), []string{"status"}, 5, false,
		); err != nil {
			t.Fatalf("expected nil for plain error, got %v", err)
		}
	})
}
