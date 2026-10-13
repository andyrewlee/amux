package git

import (
	"context"
	"testing"
)

// TestWorktreeListPaths_LiteralBytes pins the shared porcelain-path
// extraction: only the line terminator is stripped, never the payload. A
// path may legally begin or end with spaces, and both worktree-list readers
// (parseWorktreeList and isRegisteredWorktreeCtx) must classify identically.
func TestWorktreeListPaths_LiteralBytes(t *testing.T) {
	output := "worktree /repo\nbranch refs/heads/main\n\n" +
		"worktree /ws/ padded \nHEAD abc123\ndetached\n\n" +
		"worktree /repo2\nbare\n"
	got := worktreeListPaths(output)
	want := []string{"/repo", "/ws/ padded ", "/repo2"}
	if len(got) != len(want) {
		t.Fatalf("worktreeListPaths() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("worktreeListPaths()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestIsRegisteredWorktreeCtx_WhitespacePathAgreesWithParser proves the
// registration check and the discovery parser classify the same literal
// path — a whitespace-padded path must register in both or neither. The old
// TrimSpace reader registered the trimmed spelling, disagreeing with
// parseWorktreeList's literal path and wedging deletes of such paths.
func TestIsRegisteredWorktreeCtx_WhitespacePathAgreesWithParser(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() { runGitCtx = origRunGitCtx }()

	paddedPath := "/tmp/ws/padded "
	output := "worktree " + paddedPath + "\nHEAD abc123\ndetached\n\n"
	runGitCtx = func(context.Context, string, ...string) (string, error) {
		return output, nil
	}

	parsed := parseWorktreeList(output, "/tmp/repo")
	if len(parsed) != 1 || parsed[0].Root != paddedPath {
		t.Fatalf("parseWorktreeList roots = %+v, want single %q", parsed, paddedPath)
	}

	registered, err := isRegisteredWorktreeCtx(context.Background(), "/tmp/repo", paddedPath)
	if err != nil {
		t.Fatalf("isRegisteredWorktreeCtx() error = %v", err)
	}
	if !registered {
		t.Fatal("isRegisteredWorktreeCtx disagreed with parseWorktreeList on a whitespace-padded path")
	}
}

// TestRemoveWorkspace_ArgvTerminator asserts `git worktree remove` carries
// `--` before the path operand so a path beginning with `-` is never parsed
// as an option.
func TestRemoveWorkspace_ArgvTerminator(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() { runGitCtx = origRunGitCtx }()

	workspacePath := "-forged"
	var removeArgs []string
	runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		switch args[1] {
		case "list":
			return "worktree " + workspacePath + "\nHEAD abc\ndetached\n\n", nil
		case "remove":
			removeArgs = append([]string(nil), args...)
			return "", nil
		default:
			return "", nil
		}
	}

	if err := RemoveWorkspace("/tmp/repo", workspacePath); err != nil {
		t.Fatalf("RemoveWorkspace() error = %v", err)
	}
	want := []string{"worktree", "remove", "--force", "--", workspacePath}
	if len(removeArgs) != len(want) {
		t.Fatalf("remove argv = %q, want %q", removeArgs, want)
	}
	for i := range want {
		if removeArgs[i] != want[i] {
			t.Fatalf("remove argv = %q, want %q", removeArgs, want)
		}
	}
}
