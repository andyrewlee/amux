package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover the hook/fsmonitor hardening applied by hardenedGitArgs:
// amux-internal git runs must not execute repo-controlled programs (hooks,
// core.fsmonitor) unless the user opted in via AMUX_ALLOW_GIT_HOOKS=1.
// They live in their own file to respect the 500-line file-length lint gate
// on operations_test.go.

func TestHardenedGitArgs(t *testing.T) {
	t.Run("default prepends hardening flags", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = false
		t.Cleanup(func() { allowRepoGitHooks = prev })

		args := []string{"status", "--porcelain"}
		original := append([]string(nil), args...)

		got := hardenedGitArgs(args)
		want := []string{"-c", "core.hooksPath=", "-c", "core.fsmonitor=false", "status", "--porcelain"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("hardenedGitArgs() = %q, want %q", got, want)
		}
		if strings.Join(args, "\x00") != strings.Join(original, "\x00") {
			t.Fatalf("hardenedGitArgs() mutated input args: %q, want %q", args, original)
		}
	})

	t.Run("opt-in returns args unchanged", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = true
		t.Cleanup(func() { allowRepoGitHooks = prev })

		args := []string{"status", "--porcelain"}
		got := hardenedGitArgs(args)
		if strings.Join(got, "\x00") != strings.Join(args, "\x00") {
			t.Fatalf("hardenedGitArgs() = %q, want unchanged %q", got, args)
		}
	})
}

// writePostCheckoutHookSentinel installs an executable post-checkout hook in
// repo's .git/hooks that creates a sentinel file when it runs, and returns the
// sentinel path.
func writePostCheckoutHookSentinel(t *testing.T, repo string) string {
	t.Helper()
	sentinel := filepath.Join(repo, "HOOK_RAN")
	hooksDir := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(hooks) error = %v", err)
	}
	script := "#!/bin/sh\n: > '" + sentinel + "'\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "post-checkout"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(post-checkout) error = %v", err)
	}
	return sentinel
}

func TestInternalGitRunDoesNotRunRepoHook(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("sh hook script is unix-specific")
	}

	t.Run("default suppresses repo post-checkout hook", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = false
		t.Cleanup(func() { allowRepoGitHooks = prev })

		repo := initRepo(t)
		sentinel := writePostCheckoutHookSentinel(t, repo)

		if _, err := RunGitCtx(context.Background(), repo, "checkout", "-b", "hook-check"); err != nil {
			t.Fatalf("RunGitCtx(checkout) error = %v", err)
		}
		if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected repo post-checkout hook to be suppressed; sentinel stat err = %v", err)
		}
	})

	t.Run("opt-in re-enables repo post-checkout hook", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = true
		t.Cleanup(func() { allowRepoGitHooks = prev })

		repo := initRepo(t)
		sentinel := writePostCheckoutHookSentinel(t, repo)

		if _, err := RunGitCtx(context.Background(), repo, "checkout", "-b", "hook-check"); err != nil {
			t.Fatalf("RunGitCtx(checkout) error = %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("expected post-checkout hook to run under opt-in; sentinel stat err = %v", err)
		}
	})
}
