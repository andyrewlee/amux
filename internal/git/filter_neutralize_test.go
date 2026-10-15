package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveFilterDrivers_IncludedConfigDetected — a delivered .git/config can
// hide filter config behind `[include] path=...`; without --includes the
// enumeration saw nothing while `git add` still executed the driver.
func TestLiveFilterDrivers_IncludedConfigDetected(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	included := filepath.Join(root, ".git", "inc.conf")
	if err := os.WriteFile(included, []byte("[filter \"pwn\"]\n\tclean = cat >/dev/null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "--local", "include.path", "inc.conf")

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) == 0 {
		t.Fatal("filter driver delivered via include.path was not enumerated")
	}
	if live[0].driver != "pwn" {
		t.Fatalf("driver = %q, want pwn", live[0].driver)
	}
}

// TestLiveFilterDrivers_WorktreeConfigDetected — extensions.worktreeConfig
// lets a delivered .git ship filter config in config.worktree, invisible to
// a --local enumeration.
func TestLiveFilterDrivers_WorktreeConfigDetected(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	runGit(t, root, "config", "--local", "extensions.worktreeConfig", "true")
	runGit(t, root, "config", "--worktree", "filter.pwn.clean", "cat >/dev/null")

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) == 0 {
		t.Fatal("filter driver delivered via config.worktree was not enumerated")
	}
}

// TestCreateWorkspace_FilterDriverRefused_IncludedConfig — end-to-end: the
// gate must refuse a worktree add when the driver hides behind include.path.
func TestCreateWorkspace_FilterDriverRefused_IncludedConfig(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	included := filepath.Join(root, ".git", "inc.conf")
	if err := os.WriteFile(included, []byte("[filter \"pwn\"]\n\tclean = touch "+probe+"\n\tsmudge = touch "+probe+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "--local", "include.path", "inc.conf")

	_, err := CreateWorkspaceWithResult(root, filepath.Join(t.TempDir(), "ws"), "new-branch", "main")
	if err == nil {
		t.Fatal("expected a refusal, got a clean worktree add")
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
	if _, statErr := os.Stat(probe); !os.IsNotExist(statErr) {
		t.Fatal("the included filter driver EXECUTED — the probe file exists")
	}
}

// TestNeutralize_StatusDoesNotExecute — the read path: an attributed
// repo-local clean filter would execute during `git status`'s index
// refresh (the delivered index's stat data never matches). The neutralizing
// -c overrides must suppress it.
func TestNeutralize_StatusDoesNotExecute(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "touch "+probe)
	// Dirty a tracked file so the clean filter would engage on refresh.
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RunGitCtx(context.Background(), root, "status", "--porcelain=v1", "-z", "-u"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatal("filter driver EXECUTED during status — neutralization failed")
	}
}

// TestNeutralize_CacheInvalidatesOnConfigChange — the mtime key must catch
// a config edit between calls: a driver added after the first call is
// neutralized on the second.
func TestNeutralize_CacheInvalidatesOnConfigChange(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	ctx := context.Background()

	if _, err := RunGitCtx(ctx, root, "status", "--porcelain=v1"); err != nil {
		t.Fatalf("status: %v", err)
	}
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "touch "+probe)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RunGitCtx(ctx, root, "status", "--porcelain=v1"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatal("driver EXECUTED after config change — cache did not invalidate")
	}
}

// TestNeutralize_DiffDoesNotExecute — the unstaged diff read path runs the
// same clean-filter mechanism.
func TestNeutralize_DiffDoesNotExecute(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "touch "+probe)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RunGitCtx(context.Background(), root, "diff", "--", "tracked.txt"); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if _, err := os.Stat(probe); !os.IsNotExist(err) {
		t.Fatal("filter driver EXECUTED during diff — neutralization failed")
	}
}

// TestNeutralize_GlobalFilterNotNeutralized — a globally configured driver
// (legit `git lfs install`) must not be neutralized: repo-scope enum only.
func TestNeutralize_GlobalFilterNotNeutralized(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "lfslike")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	globalCfg := "[filter \"lfslike\"]\n\tclean = cat >/dev/null\n"
	if err := os.WriteFile(filepath.Join(tmpHome, ".gitconfig"), []byte(globalCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	args, err := neutralizingFilterArgs(context.Background(), root)
	if err != nil {
		t.Fatalf("neutralizingFilterArgs: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("global driver produced neutralizer args: %v", args)
	}
}

// TestAbortMerge_FilterDriverRefused — merge --abort's internal reset --hard
// executes smudge filters; the gate must fire before git runs.
func TestAbortMerge_FilterDriverRefused(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.smudge", "cat >/dev/null")

	err := AbortMerge(context.Background(), root)
	if err == nil {
		t.Fatal("expected a refusal, got a clean abort")
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
}
