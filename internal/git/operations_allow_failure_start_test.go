package git

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunGitAllowFailureCtxStartErrorIncludesCause mirrors
// TestRunGitCtxStartErrorIncludesCause for the allow-failure helper: a bad
// working directory is a launch failure, not a successful empty result.
func TestRunGitAllowFailureCtxStartErrorIncludesCause(t *testing.T) {
	skipIfNoGit(t)

	missingDir := filepath.Join(t.TempDir(), "missing")
	out, err := RunGitAllowFailureCtx(context.Background(), missingDir, "status")
	if err == nil {
		t.Fatal("expected invalid working directory error, got nil")
	}
	if out != "" {
		t.Fatalf("expected empty output on start failure, got %q", out)
	}
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected structured git error, got %T", err)
	}
	if len(gitErr.Args) != 1 || gitErr.Args[0] != "status" {
		t.Fatalf("gitErr.Args = %v, want [status]", gitErr.Args)
	}
	if gitErr.ExitCode != -1 {
		t.Fatalf("gitErr.ExitCode = %d, want -1 (process did not run)", gitErr.ExitCode)
	}
	if gitErr.Stderr != "" {
		t.Fatalf("expected empty stderr on start failure, got %q", gitErr.Stderr)
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected start failure cause (*fs.PathError), got %v", err)
	}
}

// TestRunGitAllowFailureCtxMissingExecutable covers the other launch
// failure: git itself cannot be found. PATH is scoped to this test via
// t.Setenv — no parallel marker, no global mutation.
func TestRunGitAllowFailureCtxMissingExecutable(t *testing.T) {
	skipIfNoGit(t)
	t.Setenv("PATH", t.TempDir())

	out, err := RunGitAllowFailureCtx(context.Background(), t.TempDir(), "status")
	if err == nil {
		t.Fatal("expected missing-executable error, got nil")
	}
	if out != "" {
		t.Fatalf("expected empty output on missing executable, got %q", out)
	}
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected structured git error, got %T", err)
	}
	if gitErr.ExitCode != -1 {
		t.Fatalf("gitErr.ExitCode = %d, want -1 (process did not run)", gitErr.ExitCode)
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) && !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec not-found cause, got %v", err)
	}
}
