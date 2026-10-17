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

// TestParseWorktreeListZPreservesNewlinePath pins the -z format's reason to
// exist: a path carrying a literal newline survives NUL delimiters where the
// line format corrupts it into a truncated record.
func TestParseWorktreeListZPreservesNewlinePath(t *testing.T) {
	path := "/repo/wt\nwith newline"
	zed := "worktree " + path + "\x00" +
		"HEAD abc123\x00" +
		"branch refs/heads/nl\x00" +
		"\x00" +
		"worktree /repo/main\x00" +
		"HEAD def456\x00" +
		"branch refs/heads/main\x00"

	got := parseWorktreeListFormat(zed, "/repo", true)
	if len(got) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %+v", len(got), got)
	}
	if got[0].Root != path {
		t.Fatalf("newline path corrupted under -z: %q", got[0].Root)
	}
	if got[0].Branch != "nl" {
		t.Fatalf("branch after newline path = %q, want nl", got[0].Branch)
	}
	if got[1].Root != "/repo/main" {
		t.Fatalf("second worktree Root = %q", got[1].Root)
	}

	// The same bytes in line format corrupt the record — the path splits
	// mid-field and the continuation line is absorbed as a non-field.
	line := "worktree " + path + "\nHEAD abc123\nbranch refs/heads/nl\n\n" +
		"worktree /repo/main\nHEAD def456\nbranch refs/heads/main\n"
	gotLine := parseWorktreeList(line, "/repo")
	for _, ws := range gotLine {
		if ws.Root == path {
			t.Fatalf("line format should not preserve the newline path, got %q", ws.Root)
		}
	}
}
