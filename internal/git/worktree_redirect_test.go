package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the delivered-.git worktree redirects closed by
// newGitCmd's --work-tree pin and neutralizeGitArgs's commondir refusal:
// a repo shipped with a hostile .git/config (core.worktree) or a forged
// .git/commondir must not relocate git's working tree or common dir.

func TestNewGitCmd_WorktreePinArgv(t *testing.T) {
	skipIfNoGit(t)
	stubOps(t)
	ops.allowRepoGitHooks = false

	t.Run("primary checkout pins --work-tree to abs dir", func(t *testing.T) {
		repo := initRepo(t)
		cmd := newGitCmd(repo, "status")
		abs, _ := filepath.Abs(repo)
		want := "--work-tree=" + abs
		if len(cmd.Args) < 2 || cmd.Args[1] != want {
			t.Fatalf("newGitCmd().Args = %q, want Args[1] = %q", cmd.Args, want)
		}
	})

	t.Run("bare layout is not pinned", func(t *testing.T) {
		bare := filepath.Join(t.TempDir(), "bare.git")
		if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
			t.Fatalf("git init --bare: %v\n%s", err, out)
		}
		cmd := newGitCmd(bare, "worktree", "list")
		for _, a := range cmd.Args {
			if strings.HasPrefix(a, "--work-tree=") {
				t.Fatalf("bare dir must not get --work-tree: %q", cmd.Args)
			}
		}
	})

	t.Run("linked worktree root pins to its own root", func(t *testing.T) {
		repo := initRepo(t)
		link := filepath.Join(t.TempDir(), "linked")
		runGit(t, repo, "worktree", "add", link, "-b", "lb")
		cmd := newGitCmd(link, "status")
		abs, _ := filepath.Abs(link)
		want := "--work-tree=" + abs
		if len(cmd.Args) < 2 || cmd.Args[1] != want {
			t.Fatalf("newGitCmd(linked).Args = %q, want Args[1] = %q", cmd.Args, want)
		}
	})

	t.Run("opt-in leaves config to the repo", func(t *testing.T) {
		ops.allowRepoGitHooks = true
		repo := initRepo(t)
		cmd := newGitCmd(repo, "status")
		for _, a := range cmd.Args {
			if strings.HasPrefix(a, "--work-tree=") {
				t.Fatalf("opt-in must not pin --work-tree: %q", cmd.Args)
			}
		}
	})
}

func TestDeliveredCoreWorktree_CannotRedirectReadsOrWrites(t *testing.T) {
	skipIfNoGit(t)
	stubOps(t)
	ops.allowRepoGitHooks = false

	repo := initRepo(t)
	evil := filepath.Join(t.TempDir(), "evil")
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed a tracked file the redirect would materialize under evil/ on
	// checkout-like writes — committed before forging the config so the
	// fixture's own git calls aren't redirected.
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "seed.txt")
	runGit(t, repo, "commit", "-m", "seed")
	// Delivered config points the worktree at an attacker-chosen dir.
	runGit(t, repo, "config", "core.worktree", evil)

	top, err := RunGitCtx(context.Background(), repo, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("rev-parse --show-toplevel: %v", err)
	}
	resolvedTop, _ := filepath.EvalSymlinks(top)
	resolvedRepo, _ := filepath.EvalSymlinks(repo)
	if resolvedTop != resolvedRepo {
		t.Fatalf("worktree redirected: toplevel = %q, want %q", top, repo)
	}
	if _, err := RunGitCtx(context.Background(), repo, "checkout", "-f", "HEAD"); err != nil {
		t.Fatalf("checkout -f: %v", err)
	}
	// The pinned checkout writes only inside the real worktree.
	if _, err := os.Stat(filepath.Join(evil, "seed.txt")); !os.IsNotExist(err) {
		t.Fatalf("write redirected into delivered core.worktree target: evil/seed.txt exists")
	}
}

func TestDeliveredCommondir_Refused(t *testing.T) {
	skipIfNoGit(t)
	stubOps(t)
	ops.allowRepoGitHooks = false

	repo := initRepo(t)
	// A commondir file inside a checkout's .git dir is never written by git
	// itself (it lives in <common>/worktrees/<id>/commondir) — presence here
	// means a delivered .git redirecting the common dir.
	if err := os.WriteFile(filepath.Join(repo, ".git", "commondir"), []byte(filepath.Join(t.TempDir(), "forged")), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RunGitCtx(context.Background(), repo, "status", "--porcelain"); err == nil {
		t.Fatal("expected forged-commondir refusal")
	} else if !strings.Contains(err.Error(), "commondir") {
		t.Fatalf("error should name the commondir refusal, got: %v", err)
	}
	if IsGitRepository(repo) {
		t.Fatal("IsGitRepository must refuse a forged-commondir repo")
	}

	// Removing the forged file restores normal operation.
	if err := os.Remove(filepath.Join(repo, ".git", "commondir")); err != nil {
		t.Fatal(err)
	}
	if !IsGitRepository(repo) {
		t.Fatal("repo should be usable again once the forged commondir is gone")
	}
}

func TestLinkedWorktree_CommondirUntouched(t *testing.T) {
	skipIfNoGit(t)
	stubOps(t)
	ops.allowRepoGitHooks = false

	repo := initRepo(t)
	link := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", link, "-b", "lb")

	// A linked worktree's gitdir legitimately carries commondir — reached via
	// the .git file, not a .git dir — so ops there must keep working.
	if _, err := RunGitCtx(context.Background(), link, "status", "--porcelain"); err != nil {
		t.Fatalf("linked worktree status must not hit the commondir refusal: %v", err)
	}
}
