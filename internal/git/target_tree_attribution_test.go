package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWithHostileBranchAttrs builds main (clean — no .gitattributes) plus a
// `hostile` branch whose tree commits .gitattributes binding every path to
// <attr>=<driver>. The configured-driver half is planted by the caller.
// This is the target-tree shape: the CURRENT index attributes nothing, so a
// gate that only reads the current tree sees no live binding — but a
// worktree add or merge writing the hostile tree executes the driver.
func repoWithHostileBranchAttrs(t *testing.T, attr, driver string) string {
	t.Helper()
	root := initRepo(t)
	commitFile(t, root, "tracked.txt", "content\n", "add tracked")
	runGit(t, root, "checkout", "-b", "hostile")
	commitFile(t, root, ".gitattributes", "* "+attr+"="+driver+"\n", "hostile attrs")
	runGit(t, root, "checkout", "-")
	return root
}

// TestLiveFilterDrivers_TargetTreeAttributionDetected is the enumeration
// half: check-attr --source must surface a driver attributed only by the
// branch tree being checked out — the current index reports nothing.
func TestLiveFilterDrivers_TargetTreeAttributionDetected(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "filter", "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	cur, err := liveFilterDrivers(context.Background(), root)
	if err != nil {
		t.Fatalf("liveFilterDrivers(current): %v", err)
	}
	if len(cur) != 0 {
		t.Fatalf("current index should attribute nothing, got %v", cur)
	}
	live, err := liveFilterDrivers(context.Background(), root, "hostile")
	if err != nil {
		t.Fatalf("liveFilterDrivers(hostile): %v", err)
	}
	if len(live) == 0 {
		t.Fatal("target-tree-attributed filter driver not reported")
	}
	if live[0].driver != "pwn" {
		t.Fatalf("driver = %q, want pwn", live[0].driver)
	}
}

// TestCreateWorkspace_FilterDriverRefused_TargetTreeAttrs is the exploit
// case: `worktree add <base>` applies the BASE tree's .gitattributes — a
// branch carrying attribution for a configured filter driver would exec
// during the create-time checkout while the current tree stays clean.
func TestCreateWorkspace_FilterDriverRefused_TargetTreeAttrs(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "filter", "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.smudge", "touch /tmp/amux-tt-pwned")

	wsPath := filepath.Join(t.TempDir(), "ws")
	_, err := CreateWorkspaceWithResult(root, wsPath, "wt-from-hostile", "hostile")
	if err == nil {
		t.Fatal("expected refusal, got a clean create")
	}
	if !strings.Contains(err.Error(), "refus") {
		t.Fatalf("unexpected error (want refusal): %v", err)
	}
	if _, statErr := os.Stat("/tmp/amux-tt-pwned"); statErr == nil {
		t.Fatal("filter driver executed during worktree add — target-tree gate failed")
	}
}

// TestAttachWorkspace_FilterDriverRefused_TargetTreeAttrs covers the
// sibling entry point — attach checks out the named branch the same way.
func TestAttachWorkspace_FilterDriverRefused_TargetTreeAttrs(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "filter", "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.smudge", "true")

	wsPath := filepath.Join(t.TempDir(), "ws")
	if err := AttachWorkspaceWorktree(root, wsPath, "hostile"); err == nil {
		t.Fatal("expected refusal, got a clean attach")
	} else if !strings.Contains(err.Error(), "refus") {
		t.Fatalf("unexpected error (want refusal): %v", err)
	}
}

// TestMergeWorkspaceBranch_FilterDriverRefused_IncomingTreeAttrs: merging a
// branch whose tree adds `.gitattributes` attribution writes files under
// the merged attribute stack — the incoming tree's binding executes.
func TestMergeWorkspaceBranch_FilterDriverRefused_IncomingTreeAttrs(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "filter", "pwn")
	runGit(t, root, "config", "--local", "filter.pwn.clean", "cat >/dev/null")

	err := MergeWorkspaceBranch(context.Background(), root, "hostile")
	if err == nil {
		t.Fatal("expected refusal, got a clean merge")
	}
	if !strings.Contains(err.Error(), "refus") {
		t.Fatalf("unexpected error (want refusal): %v", err)
	}
}

// TestMergeWorkspaceBranch_MergeDriverRefused_IncomingTreeAttrs: the same
// hole for merge.<name>.driver — the incoming tree attributes the driver.
func TestMergeWorkspaceBranch_MergeDriverRefused_IncomingTreeAttrs(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "merge", "evil")
	runGit(t, root, "config", "merge.evil.driver", "touch %P.evil")

	err := MergeWorkspaceBranch(context.Background(), root, "hostile")
	if err == nil {
		t.Fatal("expected refusal, got a clean merge")
	}
	if !strings.Contains(err.Error(), "refus") {
		t.Fatalf("unexpected error (want refusal): %v", err)
	}
}

// TestCreateWorkspace_TargetTreeUnconfiguredDriver_Passes is the positive
// control: the target tree attributes a driver name that no config defines
// — nothing can execute, so the create proceeds.
func TestCreateWorkspace_TargetTreeUnconfiguredDriver_Passes(t *testing.T) {
	skipIfNoGit(t)
	root := repoWithHostileBranchAttrs(t, "filter", "ghost")

	wsPath := filepath.Join(t.TempDir(), "ws")
	if _, err := CreateWorkspaceWithResult(root, wsPath, "wt-clean", "hostile"); err != nil {
		t.Fatalf("unconfigured driver should not block create: %v", err)
	}
}
