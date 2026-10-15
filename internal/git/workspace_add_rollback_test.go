package git

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// stubTimedOutWorktreeAdd makes every `worktree add` hang until its context
// expires (a checkout killed by the timeout) and records all other git calls.
func stubTimedOutWorktreeAdd(t *testing.T, failFirstAddWith error) *[]string {
	t.Helper()
	stubOps(t)
	stubFilterDriverGate(t)
	ops.worktreeAddTimeout = 10 * time.Millisecond

	var calls []string
	adds := 0
	ops.runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
			adds++
			if adds == 1 && failFirstAddWith != nil {
				return "", failFirstAddWith
			}
			<-ctx.Done()
			return "", &Error{Command: "worktree add", ExitCode: -1, Err: ctx.Err()}
		}
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	return &calls
}

func TestCreateWorkspaceRollsBackTimedOutAdd(t *testing.T) {
	calls := stubTimedOutWorktreeAdd(t, nil)

	branchCreated, err := CreateWorkspaceWithResult("/tmp/repo", "/tmp/ws", "feature-ws", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("CreateWorkspaceWithResult() error = %v, want timeout", err)
	}
	if branchCreated {
		t.Fatalf("branchCreated = true on error")
	}
	want := []string{"worktree remove --force --force -- /tmp/ws", "branch -D feature-ws"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("rollback calls = %q, want %q", *calls, want)
	}
}

func TestCreateWorkspaceRollbackKeepsPreexistingBranch(t *testing.T) {
	branchExists := &Error{Command: "worktree add", ExitCode: 128, Stderr: "fatal: a branch named 'feature-ws' already exists", Err: errors.New("exit status 128")}
	calls := stubTimedOutWorktreeAdd(t, branchExists)

	if err := CreateWorkspace("/tmp/repo", "/tmp/ws", "feature-ws", "HEAD"); err == nil {
		t.Fatalf("CreateWorkspace() error = nil, want timeout")
	}
	want := []string{"worktree remove --force --force -- /tmp/ws"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("rollback calls = %q, want %q (the branch predates the call)", *calls, want)
	}
}

func TestAttachWorkspaceRollsBackTimedOutAdd(t *testing.T) {
	calls := stubTimedOutWorktreeAdd(t, nil)

	if err := AttachWorkspaceWorktree("/tmp/repo", "/tmp/ws", "feature-ws"); err == nil {
		t.Fatalf("AttachWorkspaceWorktree() error = nil, want timeout")
	}
	want := []string{"worktree remove --force --force -- /tmp/ws"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Fatalf("rollback calls = %q, want %q", *calls, want)
	}
}

func TestCreateWorkspaceNonTimeoutFailureSkipsRollback(t *testing.T) {
	stubOps(t)
	call := 0
	ops.runGitCtx = func(_ context.Context, _ string, args ...string) (string, error) {
		call++
		if call > 1 {
			t.Fatalf("unexpected git call %q after a non-timeout failure", strings.Join(args, " "))
		}
		return "", &Error{Command: "worktree add", ExitCode: 128, Stderr: "fatal: '/tmp/ws' already exists", Err: errors.New("exit status 128")}
	}

	if err := CreateWorkspace("/tmp/repo", "/tmp/ws", "feature-ws", "HEAD"); err == nil {
		t.Fatalf("CreateWorkspace() error = nil, want path-exists error")
	}
}
