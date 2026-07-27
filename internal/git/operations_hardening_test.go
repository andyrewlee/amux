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
)

// These tests cover the hook/fsmonitor/gpg-sign hardening applied by
// hardenedGitArgs: amux-internal git runs must not execute repo-controlled
// programs (hooks, core.fsmonitor, gpg.program via commit.gpgsign) unless the
// user opted in via AMUX_ALLOW_GIT_HOOKS=1. They live in their own file to
// respect the 500-line file-length lint gate on operations_test.go.

func TestHardenedGitArgs(t *testing.T) {
	t.Run("default prepends hardening flags", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = false
		t.Cleanup(func() { allowRepoGitHooks = prev })

		args := []string{"status", "--porcelain"}
		original := append([]string(nil), args...)

		got := hardenedGitArgs(args)
		want := []string{
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-c", "commit.gpgsign=false",
			"status", "--porcelain",
		}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("hardenedGitArgs() = %q, want %q", got, want)
		}
		if strings.Join(args, "\x00") != strings.Join(original, "\x00") {
			t.Fatalf("hardenedGitArgs() mutated input args: %q, want %q", args, original)
		}
	})

	t.Run("default includes commit.gpgsign=false", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = false
		t.Cleanup(func() { allowRepoGitHooks = prev })

		got := hardenedGitArgs([]string{"commit", "-m", "msg"})
		joined := strings.Join(got, "\x00")
		if !strings.Contains(joined, "-c\x00commit.gpgsign=false") {
			t.Fatalf("hardenedGitArgs(commit) = %q, want it to contain -c commit.gpgsign=false", got)
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

	t.Run("opt-in also restores gpg-signing (no commit.gpgsign override)", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = true
		t.Cleanup(func() { allowRepoGitHooks = prev })

		got := hardenedGitArgs([]string{"commit", "-m", "msg"})
		joined := strings.Join(got, "\x00")
		if strings.Contains(joined, "commit.gpgsign") {
			t.Fatalf("hardenedGitArgs(commit) under opt-in = %q, want no commit.gpgsign override at all", got)
		}
	})
}

// TestNoHooksPathIsUnwritable guards the property that keeps workspace roots
// clean: hook-disabling must not be expressed as an empty core.hooksPath.
// git-lfs re-installs its hooks on every filter invocation (filter-process and
// smudge) using whatever core.hooksPath says, so an empty value makes it write
// pre-push and friends into the current directory — i.e. into the root of every
// new workspace.
func TestNoHooksPathIsUnwritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("amux is Linux/macOS-only; /dev/null is unix-specific")
	}

	if noHooksPath == "" {
		t.Fatal("noHooksPath is empty; tools that read core.hooksPath would resolve it to the working directory and litter hook files there")
	}
	if !filepath.IsAbs(noHooksPath) {
		t.Fatalf("noHooksPath = %q, want an absolute path so it never resolves relative to the working directory", noHooksPath)
	}
	if err := os.WriteFile(filepath.Join(noHooksPath, "pre-push"), []byte("#!/bin/sh\n"), 0o755); err == nil {
		t.Fatalf("wrote a hook into noHooksPath (%q); it must reject hook installs", noHooksPath)
	}
}

// TestCreateWorkspaceLeavesNoStrayLFSHooks is the end-to-end guard for the same
// property against the real git-lfs binary: creating a workspace in an
// lfs-tracked repo must leave the worktree root clean while the lfs smudge
// filter still runs.
func TestCreateWorkspaceLeavesNoStrayLFSHooks(t *testing.T) {
	skipIfNoGit(t)
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs not installed")
	}

	prev := allowRepoGitHooks
	allowRepoGitHooks = false
	t.Cleanup(func() { allowRepoGitHooks = prev })

	repo := initRepo(t)
	runGit(t, repo, "lfs", "track", "*.bin")
	if err := os.WriteFile(filepath.Join(repo, "a.bin"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write a.bin: %v", err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "add lfs file")

	base := currentBranchForTest(t, repo)
	ws := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorkspace(repo, ws, "lfs-check", base); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	// git-lfs re-installs these four hooks on every filter invocation, using
	// whatever core.hooksPath resolves to.
	for _, stray := range []string{"pre-push", "post-checkout", "post-commit", "post-merge"} {
		if _, err := os.Stat(filepath.Join(ws, stray)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stray git-lfs hook %q in workspace root; stat err = %v", stray, err)
		}
	}

	// Hardening must not disable the lfs clean/smudge filters themselves.
	got, err := os.ReadFile(filepath.Join(ws, "a.bin"))
	if err != nil {
		t.Fatalf("read a.bin: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("a.bin = %q, want smudged content %q", got, "hello\n")
	}
}

func currentBranchForTest(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
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

// writeGpgProgramSentinel repo-locally configures commit.gpgsign=true and
// gpg.program to point at a script that creates a sentinel file when
// invoked (then exits non-zero, so a real signing attempt fails cleanly
// instead of hanging or fabricating a signature). Returns the sentinel path.
func writeGpgProgramSentinel(t *testing.T, repo string) string {
	t.Helper()
	sentinel := filepath.Join(repo, "GPG_RAN")
	scriptPath := filepath.Join(repo, "fake-gpg.sh")
	script := "#!/bin/sh\n: > '" + sentinel + "'\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake-gpg.sh) error = %v", err)
	}
	runGit(t, repo, "config", "commit.gpgsign", "true")
	runGit(t, repo, "config", "gpg.program", scriptPath)
	return sentinel
}

func TestInternalGitCommitDoesNotRunRepoGpgProgram(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("sh gpg-program script is unix-specific")
	}

	t.Run("default suppresses repo gpg.program on commit", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = false
		t.Cleanup(func() { allowRepoGitHooks = prev })

		repo := initRepo(t)
		sentinel := writeGpgProgramSentinel(t, repo)

		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatalf("write file.txt: %v", err)
		}
		if _, err := RunGitCtx(context.Background(), repo, "add", "-A"); err != nil {
			t.Fatalf("RunGitCtx(add) error = %v", err)
		}
		if _, err := RunGitCtx(context.Background(), repo, "commit", "-m", "no sign"); err != nil {
			t.Fatalf("RunGitCtx(commit) error = %v", err)
		}
		if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected repo gpg.program to be suppressed; sentinel stat err = %v", err)
		}
	})

	t.Run("opt-in re-enables repo gpg.program on commit", func(t *testing.T) {
		prev := allowRepoGitHooks
		allowRepoGitHooks = true
		t.Cleanup(func() { allowRepoGitHooks = prev })

		repo := initRepo(t)
		sentinel := writeGpgProgramSentinel(t, repo)

		if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatalf("write file.txt: %v", err)
		}
		if _, err := RunGitCtx(context.Background(), repo, "add", "-A"); err != nil {
			t.Fatalf("RunGitCtx(add) error = %v", err)
		}
		// The fake gpg program exits non-zero, so the commit itself fails under
		// opt-in — we only assert it was invoked (sentinel created), proving
		// commit.gpgsign is not force-disabled when the user opted in.
		_, _ = RunGitCtx(context.Background(), repo, "commit", "-m", "sign attempt")
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("expected repo gpg.program to run under opt-in; sentinel stat err = %v", err)
		}
	})
}
