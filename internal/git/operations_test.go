package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestGetRemotePushURL(t *testing.T) {
	skipIfNoGit(t)

	t.Run("returns explicit push url", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "remote", "add", "origin", "https://example.com/upstream.git")
		runGit(t, repo, "remote", "set-url", "--push", "origin", "git@example.com:fork/repo.git")

		remote, err := GetRemotePushURL(repo, "origin")
		if err != nil {
			t.Fatalf("GetRemotePushURL() error = %v", err)
		}
		if remote != "git@example.com:fork/repo.git" {
			t.Fatalf("GetRemotePushURL() = %s, want git@example.com:fork/repo.git", remote)
		}
	})

	t.Run("falls back to fetch url", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "remote", "add", "origin", "https://example.com/repo.git")

		remote, err := GetRemotePushURL(repo, "origin")
		if err != nil {
			t.Fatalf("GetRemotePushURL() error = %v", err)
		}
		if remote != "https://example.com/repo.git" {
			t.Fatalf("GetRemotePushURL() = %s, want https://example.com/repo.git", remote)
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

func TestResolveCurrentBranchOrFallback(t *testing.T) {
	skipIfNoGit(t)

	t.Run("uses current branch when available", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")

		got, err := ResolveCurrentBranchOrFallback(repo, "main")
		if err != nil {
			t.Fatalf("ResolveCurrentBranchOrFallback() error = %v", err)
		}
		if got != "feature" {
			t.Fatalf("ResolveCurrentBranchOrFallback() = %q, want %q", got, "feature")
		}
	})

	t.Run("falls back when branch lookup fails", func(t *testing.T) {
		got, err := ResolveCurrentBranchOrFallback(t.TempDir(), "feature")
		if err != nil {
			t.Fatalf("ResolveCurrentBranchOrFallback() error = %v", err)
		}
		if got != "feature" {
			t.Fatalf("ResolveCurrentBranchOrFallback() = %q, want %q", got, "feature")
		}
	})

	t.Run("detached HEAD errors even with fallback", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "-b", "feature")
		runGit(t, repo, "checkout", "--detach", "HEAD")

		if _, err := ResolveCurrentBranchOrFallback(repo, "feature"); !errors.Is(err, ErrDetachedHEAD) {
			t.Fatalf("ResolveCurrentBranchOrFallback() error = %v, want %v", err, ErrDetachedHEAD)
		}
	})

	t.Run("detached HEAD without usable fallback errors", func(t *testing.T) {
		repo := initRepo(t)
		runGit(t, repo, "checkout", "--detach", "HEAD")

		if _, err := ResolveCurrentBranchOrFallback(repo, "HEAD"); err == nil {
			t.Fatal("ResolveCurrentBranchOrFallback() error = nil, want error")
		}
	})
}

func TestGetRemoteURLExpandsInsteadOfRewrite(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	runGit(t, repo, "config", "url.https://github.com/.insteadOf", "gh:")
	runGit(t, repo, "remote", "add", "origin", "gh:openai/amux.git")

	remote, err := GetRemoteURL(repo, "origin")
	if err != nil {
		t.Fatalf("GetRemoteURL() error = %v", err)
	}
	if remote != "https://github.com/openai/amux.git" {
		t.Fatalf("GetRemoteURL() = %s, want https://github.com/openai/amux.git", remote)
	}
}

func TestGetRemotePushURLExpandsPushInsteadOfRewrite(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	runGit(t, repo, "config", "url.https://github.com/.insteadOf", "gh:")
	runGit(t, repo, "config", "url.git@github.com:.pushInsteadOf", "gh:")
	runGit(t, repo, "remote", "add", "origin", "gh:openai/amux.git")

	remote, err := GetRemotePushURL(repo, "origin")
	if err != nil {
		t.Fatalf("GetRemotePushURL() error = %v", err)
	}
	if remote != "git@github.com:openai/amux.git" {
		t.Fatalf("GetRemotePushURL() = %s, want git@github.com:openai/amux.git", remote)
	}
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
