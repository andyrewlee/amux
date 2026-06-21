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

func TestIsGitRepository(t *testing.T) {
	skipIfNoGit(t)

	t.Run("valid repo", func(t *testing.T) {
		repo := initRepo(t)
		if !IsGitRepository(repo) {
			t.Fatalf("IsGitRepository() should return true for repo")
		}
	})

	t.Run("non-repo directory", func(t *testing.T) {
		nonRepo := t.TempDir()
		if IsGitRepository(nonRepo) {
			t.Fatalf("IsGitRepository() should return false for non-repo")
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		if IsGitRepository("/nonexistent/path/to/repo") {
			t.Fatalf("IsGitRepository() should return false for non-existent path")
		}
	})
}

func TestGetRepoRoot(t *testing.T) {
	skipIfNoGit(t)

	t.Run("valid repo", func(t *testing.T) {
		repo := initRepo(t)
		root, err := GetRepoRoot(repo)
		if err != nil {
			t.Fatalf("GetRepoRoot() error = %v", err)
		}

		// Normalize symlinks for comparison
		rootEval := root
		if eval, err := filepath.EvalSymlinks(root); err == nil {
			rootEval = eval
		}
		repoEval := repo
		if eval, err := filepath.EvalSymlinks(repo); err == nil {
			repoEval = eval
		}
		if rootEval != repoEval {
			t.Fatalf("GetRepoRoot() = %s, want %s", rootEval, repoEval)
		}
	})

	t.Run("subdirectory of repo", func(t *testing.T) {
		repo := initRepo(t)
		subdir := filepath.Join(repo, "subdir")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}

		root, err := GetRepoRoot(subdir)
		if err != nil {
			t.Fatalf("GetRepoRoot() error = %v", err)
		}

		rootEval, _ := filepath.EvalSymlinks(root)
		repoEval, _ := filepath.EvalSymlinks(repo)
		if rootEval != repoEval {
			t.Fatalf("GetRepoRoot() from subdir = %s, want %s", rootEval, repoEval)
		}
	})

	t.Run("non-repo directory", func(t *testing.T) {
		nonRepo := t.TempDir()
		_, err := GetRepoRoot(nonRepo)
		if err == nil {
			t.Fatalf("GetRepoRoot() should fail for non-repo")
		}
	})
}

func TestGetCurrentBranch(t *testing.T) {
	skipIfNoGit(t)

	t.Run("main branch", func(t *testing.T) {
		repo := initRepo(t)
		branch, err := GetCurrentBranch(repo)
		if err != nil {
			t.Fatalf("GetCurrentBranch() error = %v", err)
		}
		if branch != "main" {
			t.Fatalf("GetCurrentBranch() = %s, want main", branch)
		}
	})

	t.Run("feature branch", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature-test")

		branch, err := GetCurrentBranch(repo)
		if err != nil {
			t.Fatalf("GetCurrentBranch() error = %v", err)
		}
		if branch != "feature-test" {
			t.Fatalf("GetCurrentBranch() = %s, want feature-test", branch)
		}
	})

	t.Run("non-repo directory", func(t *testing.T) {
		nonRepo := t.TempDir()
		_, err := GetCurrentBranch(nonRepo)
		if err == nil {
			t.Fatalf("GetCurrentBranch() should fail for non-repo")
		}
	})
}

func TestGetRemoteURL(t *testing.T) {
	skipIfNoGit(t)

	t.Run("existing remote", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "remote", "add", "origin", "https://example.com/repo.git")

		remote, err := GetRemoteURL(repo, "origin")
		if err != nil {
			t.Fatalf("GetRemoteURL() error = %v", err)
		}
		if remote != "https://example.com/repo.git" {
			t.Fatalf("GetRemoteURL() = %s, want https://example.com/repo.git", remote)
		}
	})

	t.Run("non-existent remote", func(t *testing.T) {
		repo := initRepo(t)
		_, err := GetRemoteURL(repo, "nonexistent")
		if err == nil {
			t.Fatalf("GetRemoteURL() should fail for non-existent remote")
		}
	})

	t.Run("non-repo directory", func(t *testing.T) {
		nonRepo := t.TempDir()
		_, err := GetRemoteURL(nonRepo, "origin")
		if err == nil {
			t.Fatalf("GetRemoteURL() should fail for non-repo")
		}
	})
}

func TestGetStatus(t *testing.T) {
	skipIfNoGit(t)

	t.Run("clean repo", func(t *testing.T) {
		repo := initRepo(t)
		status, err := GetStatus(repo)
		if err != nil {
			t.Fatalf("GetStatus() error = %v", err)
		}
		if !status.Clean {
			t.Fatalf("expected clean status, got %+v", status)
		}
	})

	t.Run("dirty repo with untracked file", func(t *testing.T) {
		repo := initRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		status, err := GetStatus(repo)
		if err != nil {
			t.Fatalf("GetStatus() error = %v", err)
		}
		if status.Clean {
			t.Fatalf("expected dirty status")
		}
		if len(status.Untracked) != 1 {
			t.Fatalf("expected 1 untracked file, got %d", len(status.Untracked))
		}
	})

	t.Run("dirty repo with modified file", func(t *testing.T) {
		repo := initRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		status, err := GetStatus(repo)
		if err != nil {
			t.Fatalf("GetStatus() error = %v", err)
		}
		if status.Clean {
			t.Fatalf("expected dirty status")
		}
	})

	t.Run("non-repo directory", func(t *testing.T) {
		nonRepo := t.TempDir()
		_, err := GetStatus(nonRepo)
		if err == nil {
			t.Fatalf("GetStatus() should fail for non-repo")
		}
	})
}

func TestRunGitCtxTimeoutError(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	_, err := RunGitCtx(ctx, repo, "status")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
	if !strings.Contains(err.Error(), "git status") {
		t.Fatalf("expected error to include command context, got %v", err)
	}
}

func TestRunGitCtxTimeoutErrorFromKilledProcess(t *testing.T) {
	skipIfNoGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("shell sleep alias is unix-specific")
	}

	repo := initRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := RunGitCtx(ctx, repo, "-c", "alias.sleep=!sleep 1", "sleep")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
	if !strings.Contains(err.Error(), "git -c alias.sleep=!sleep 1 sleep") {
		t.Fatalf("expected error to include command context, got %v", err)
	}
}

func TestGitCommandContextErrorDoesNotMaskExitErrorAfterLateCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell exit status command is unix-specific")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected exit error")
	}

	cancel()

	if ctxErr := gitCommandContextErrorWithKill(ctx, err, []string{"status"}, false); ctxErr != nil {
		t.Fatalf("expected late cancellation to preserve original exit error, got %v", ctxErr)
	}
}

func TestGitAllowFailureCommandContextErrorForWindowsPreservesOutputExitOne(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ctxErr := gitAllowFailureCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		1,
		false,
	); ctxErr != nil {
		t.Fatalf("expected allow-failure output to suppress ambiguous windows timeout mapping, got %v", ctxErr)
	}
}

func TestGitAllowFailureCommandContextErrorForWindowsWithoutKillDoesNotMapTimeout(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ctxErr := gitAllowFailureCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		0,
		false,
	)
	if ctxErr != nil {
		t.Fatalf("expected ordinary exit error to remain unmapped without kill evidence, got %v", ctxErr)
	}
}

func TestGitAllowFailureCommandContextErrorForWindowsMapsTimeoutWhenKilled(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	ctxErr := gitAllowFailureCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		1,
		true,
	)
	if !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded context error, got %v", ctxErr)
	}
}

func TestGitAllowFailureCommandContextErrorForWindowsDoesNotMapLateDeadlineWithoutKillWhenOutputCaptured(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	ctxErr := gitAllowFailureCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		1,
		false,
	)
	if ctxErr != nil {
		t.Fatalf("expected ordinary exit error to remain unmapped without kill evidence, got %v", ctxErr)
	}
}

func TestGitCommandContextErrorForWindowsWithoutKillDoesNotMapTimeout(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	ctxErr := gitCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		false,
	)
	if ctxErr != nil {
		t.Fatalf("expected ordinary exit error to remain unmapped without kill evidence, got %v", ctxErr)
	}
}

func TestGitCommandContextErrorForWindowsMapsKilledExitOne(t *testing.T) {
	skipIfNoGit(t)

	tmp := t.TempDir()
	left := filepath.Join(tmp, "left.txt")
	right := filepath.Join(tmp, "right.txt")
	if err := os.WriteFile(left, []byte("left\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(left) error = %v", err)
	}
	if err := os.WriteFile(right, []byte("right\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(right) error = %v", err)
	}

	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--", left, right)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected diff exit error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	ctxErr := gitCommandContextErrorWithKillForGOOS(
		ctx,
		"windows",
		err,
		[]string{"diff", "--no-index", "--no-color", "--", left, right},
		true,
	)
	if !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded context error, got %v", ctxErr)
	}
}

func TestIsCommandContextTerminationExitCode(t *testing.T) {
	if !isCommandContextTerminationExitCode(-1) {
		t.Fatal("expected -1 to be treated as a command-context termination exit code")
	}
	if isCommandContextTerminationExitCode(1) {
		t.Fatal("expected 1 to be preserved as a normal process exit code")
	}
	if isCommandContextTerminationExitCode(2) {
		t.Fatal("expected 2 to be preserved as a normal process exit code")
	}
}
