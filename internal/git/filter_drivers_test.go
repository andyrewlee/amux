package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoWithFilterDriver builds a repo with `* filter=<driver>` committed in
// .gitattributes plus one tracked file — the attribution half of a live
// binding. Callers add the config half themselves (or don't, to probe the
// unattributed case).
func repoWithFilterDriver(t *testing.T, driver string) string {
	t.Helper()
	root := initRepo(t)
	commitFile(t, root, ".gitattributes", "* filter="+driver+"\n", "attribute all")
	commitFile(t, root, "tracked.txt", "content\n", "add tracked")
	return root
}

// allowHooks flips the AMUX_ALLOW_GIT_HOOKS seam for one test — the same
// pattern operations_hardening_test.go uses.
func allowHooks(t *testing.T) {
	t.Helper()
	prev := allowRepoGitHooks
	allowRepoGitHooks = true
	t.Cleanup(func() { allowRepoGitHooks = prev })
}

// TestLiveFilterDrivers_AttributedRepoLocalDriverDetected is the exploit
// case: a delivered .git/config carrying filter.<name>.clean plus a tracked
// .gitattributes binding is a live exec binding — the enumeration must
// report the (driver, path) pair.
func TestLiveFilterDrivers_AttributedRepoLocalDriverDetected(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) == 0 {
		t.Fatal("attributed repo-local filter driver not reported")
	}
	if live[0].driver != "pwn" {
		t.Fatalf("driver = %q, want pwn", live[0].driver)
	}
	if live[0].path == "" {
		t.Fatal("attribution carried no path")
	}
}

// TestLiveFilterDrivers_GlobalOnlyIgnored pins the --local scope: a driver
// configured in the user's own global config (the legitimate `git lfs
// install` case) is visible to git but NOT repo-deliverable — it must not
// trip the gate.
func TestLiveFilterDrivers_GlobalOnlyIgnored(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "lfslike")

	// A fake HOME scopes the "global" write so the real ~/.gitconfig is
	// never touched.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	globalCfg := "[filter \"lfslike\"]\n\tclean = cat >/dev/null\n"
	if err := os.WriteFile(filepath.Join(tmpHome, ".gitconfig"), []byte(globalCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("global-scope driver reported live: %+v — the enumeration must not see user config", live)
	}
}

// TestLiveFilterDrivers_UnattributedIgnored proves the refusal is scoped to
// live bindings: a configured driver nothing attributes can never fire.
func TestLiveFilterDrivers_UnattributedIgnored(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)
	commitFile(t, root, "tracked.txt", "content\n", "add tracked")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("unattributed driver reported live: %+v", live)
	}
}

// TestCreateWorkspace_FilterDriverRefused — `worktree add`'s checkout runs
// attributed smudge/clean drivers before the workspace's trust gate exists;
// the op must refuse before git runs.
func TestCreateWorkspace_FilterDriverRefused(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	probe := filepath.Join(t.TempDir(), "driver-ran")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "touch "+probe)
	runGit(t, root, "config", "--local", "filter.pwn.smudge", "touch "+probe)

	_, err := CreateWorkspaceWithResult(root, filepath.Join(t.TempDir(), "ws"), "new-branch", "main")
	if err == nil {
		t.Fatal("expected a refusal, got a clean worktree add")
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
	if !strings.Contains(err.Error(), "AMUX_ALLOW_GIT_HOOKS") {
		t.Fatalf("refusal does not name the opt-out: %v", err)
	}
	if _, statErr := os.Stat(probe); !os.IsNotExist(statErr) {
		t.Fatal("the filter driver EXECUTED — the probe file exists")
	}
}

// TestAttachWorkspace_FilterDriverRefused — attach runs the same
// `worktree add` checkout; same refusal.
func TestAttachWorkspace_FilterDriverRefused(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.smudge", "cat")

	err := AttachWorkspaceWorktree(root, filepath.Join(t.TempDir(), "ws"), "main")
	if err == nil {
		t.Fatal("expected a refusal, got a clean attach")
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
}

// TestCommitAll_FilterDriverRefused — `add -A` runs attributed clean
// filters during staging; refuse before the stage executes them.
func TestCommitAll_FilterDriverRefused(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFilterDriver(t, "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat")
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := CommitAll(context.Background(), root, "test commit")
	if err == nil {
		t.Fatal("expected a refusal, got a clean commit")
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected the refusal error, got: %v", err)
	}
}

// TestMerge_FilterDriverRefused — a merge's checkout writes run attributed
// smudge drivers; refuse alongside the merge-driver check.
func TestMerge_FilterDriverRefused(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithFeatureBranch(t)
	commitFile(t, root, ".gitattributes", "* filter=pwn\n", "attribute filter")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	err := MergeWorkspaceBranch(context.Background(), root, "feature")
	if err == nil {
		t.Fatal("expected a refusal, got a clean merge")
	}
	if errors.Is(err, ErrMergeConflict) {
		t.Fatalf("refusal misreported as a conflict: %v", err)
	}
	if !strings.Contains(err.Error(), "pwn") {
		t.Fatalf("refusal does not name the driver: %v", err)
	}
}

// TestMerge_FilterDriverAllowedWithEnvOptOut — AMUX_ALLOW_GIT_HOOKS=1
// stands the gate down with the rest of the hardening; an unattributed
// (harmless cat) driver then lets the merge proceed to its normal outcome.
func TestMerge_FilterDriverAllowedWithEnvOptOut(t *testing.T) {
	skipIfNoGit(t)
	allowHooks(t)
	root := repoWithFeatureBranch(t)
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat")
	// No attribution — even opted-in, prove the path proceeds on merit.
	if err := MergeWorkspaceBranch(context.Background(), root, "feature"); err != nil {
		t.Fatalf("opted-in merge with unattributed driver failed: %v", err)
	}
}

// TestLiveFilterDrivers_BulkPathsPastBatchBoundary — the enumeration used
// to batch check-attr at 200 paths per spawn; the --stdin rewrite must
// still surface attributions sitting past that old boundary (and untracked
// dirs full of uninteresting files must not crowd them out).
func TestLiveFilterDrivers_BulkPathsPastBatchBoundary(t *testing.T) {
	skipIfNoGit(t)
	root := initRepo(t)

	// 300 unattributed files push the attributed pair past the old
	// 200-path batch boundary.
	for i := 0; i < 300; i++ {
		p := filepath.Join(root, "bulk", "f"+strconv.Itoa(i)+".txt")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(root, "late", "attributed.bin")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, root, ".gitattributes", "*.bin filter=pwn\n", "attribute bin files")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "bulk files")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	live, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("live = %+v, want exactly the late/ attribution", live)
	}
	if live[0].driver != "pwn" || live[0].path != "late/attributed.bin" {
		t.Fatalf("live[0] = %+v, want pwn @ late/attributed.bin", live[0])
	}
}
