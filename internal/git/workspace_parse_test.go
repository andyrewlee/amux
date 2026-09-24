package git

import "testing"

// parseWorktreeList must not trim whitespace from porcelain payload lines:
// git worktree list --porcelain emits paths unquoted, and a POSIX path may
// legally contain leading/trailing spaces.
func TestParseWorktreeListPreservesPathBytes(t *testing.T) {
	out := "worktree /repo/main\n" +
		"HEAD abc123\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo/wt with space \n" +
		"HEAD def456\n" +
		"branch refs/heads/sp branch\n" +
		"\n" +
		"worktree /repo/bare\n" +
		"HEAD 999\n" +
		"bare\n" +
		"\n"

	got := parseWorktreeList(out, "/repo")
	if len(got) != 2 {
		t.Fatalf("expected 2 non-bare worktrees, got %d: %+v", len(got), got)
	}
	if got[0].Root != "/repo/main" || got[0].Branch != "main" {
		t.Fatalf("first worktree = %+v", got[0])
	}
	if got[1].Root != "/repo/wt with space " {
		t.Fatalf("trailing-space path corrupted: %q", got[1].Root)
	}
	if got[1].Branch != "sp branch" {
		t.Fatalf("branch with space corrupted: %q", got[1].Branch)
	}
}
