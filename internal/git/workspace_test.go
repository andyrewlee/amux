package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		repoPath string
		want     int // number of workspaces expected
		wantBare bool
	}{
		{
			name:     "empty output",
			output:   "",
			repoPath: "/repo",
			want:     0,
		},
		{
			name: "single worktree",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

`,
			repoPath: "/home/user/myrepo",
			want:     1,
		},
		{
			name: "multiple worktrees",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/feature
HEAD def456
branch refs/heads/feature

`,
			repoPath: "/home/user/myrepo",
			want:     2,
		},
		{
			name: "bare repository filtered out",
			output: `worktree /home/user/myrepo.git
bare

worktree /home/user/.amux/workspaces/myrepo/feature
HEAD def456
branch refs/heads/feature

`,
			repoPath: "/home/user/myrepo.git",
			want:     1, // bare entry should be filtered
		},
		{
			name: "detached HEAD worktree",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/detached
HEAD def456
detached

`,
			repoPath: "/home/user/myrepo",
			want:     2, // detached worktree should be included
		},
		{
			name: "no trailing newline",
			output: `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main`,
			repoPath: "/home/user/myrepo",
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaces := parseWorktreeList(tt.output, tt.repoPath)
			if len(workspaces) != tt.want {
				t.Errorf("parseWorktreeList() returned %d workspaces, want %d", len(workspaces), tt.want)
			}
		})
	}
}

func TestParseWorktreeList_Fields(t *testing.T) {
	output := `worktree /home/user/myrepo
HEAD abc123
branch refs/heads/main

worktree /home/user/.amux/workspaces/myrepo/feature-branch
HEAD def456
branch refs/heads/feature-branch

`
	workspaces := parseWorktreeList(output, "/home/user/myrepo")

	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(workspaces))
	}

	// Check first workspace (primary)
	if workspaces[0].Root != "/home/user/myrepo" {
		t.Errorf("ws[0].Root = %q, want %q", workspaces[0].Root, "/home/user/myrepo")
	}
	if workspaces[0].Branch != "main" {
		t.Errorf("ws[0].Branch = %q, want %q", workspaces[0].Branch, "main")
	}
	if workspaces[0].Name != "myrepo" {
		t.Errorf("ws[0].Name = %q, want %q", workspaces[0].Name, "myrepo")
	}
	if workspaces[0].Repo != "/home/user/myrepo" {
		t.Errorf("ws[0].Repo = %q, want %q", workspaces[0].Repo, "/home/user/myrepo")
	}

	// Check second workspace (worktree)
	if workspaces[1].Root != "/home/user/.amux/workspaces/myrepo/feature-branch" {
		t.Errorf("ws[1].Root = %q, want %q", workspaces[1].Root, "/home/user/.amux/workspaces/myrepo/feature-branch")
	}
	if workspaces[1].Branch != "feature-branch" {
		t.Errorf("ws[1].Branch = %q, want %q", workspaces[1].Branch, "feature-branch")
	}
	if workspaces[1].Name != "feature-branch" {
		t.Errorf("ws[1].Name = %q, want %q", workspaces[1].Name, "feature-branch")
	}
}

func TestIsBranchAlreadyExistsError(t *testing.T) {
	err := errors.New("fatal: a branch named 'feature-a' already exists")
	if !isBranchAlreadyExistsError(err, "feature-a") {
		t.Fatalf("expected branch already exists error to match")
	}
	if isBranchAlreadyExistsError(err, "feature-b") {
		t.Fatalf("expected non-matching branch name to return false")
	}
	if isBranchAlreadyExistsError(errors.New("fatal: branch lock failed"), "feature-a") {
		t.Fatalf("expected unrelated branch error to return false")
	}
	if isBranchAlreadyExistsError(errors.New("fatal: already exists"), "") {
		t.Fatalf("expected empty branch name to return false")
	}
}

func TestCreateWorkspace_RetryUsesFreshContext(t *testing.T) {
	origRunGitCtx := runGitCtx
	defer func() {
		runGitCtx = origRunGitCtx
	}()

	var firstCtx context.Context
	call := 0
	runGitCtx = func(ctx context.Context, _ string, args ...string) (string, error) {
		call++
		switch call {
		case 1:
			firstCtx = ctx
			if got, want := strings.Join(args, " "), "worktree add -b feature-ws /tmp/ws HEAD"; got != want {
				t.Fatalf("first call args = %q, want %q", got, want)
			}
			return "", errors.New("fatal: a branch named 'feature-ws' already exists")
		case 2:
			if firstCtx == nil {
				t.Fatalf("expected first context to be captured")
			}
			if ctx == firstCtx {
				t.Fatalf("expected retry to use a fresh context")
			}
			if got, want := strings.Join(args, " "), "worktree add /tmp/ws feature-ws"; got != want {
				t.Fatalf("retry args = %q, want %q", got, want)
			}
			return "", nil
		default:
			t.Fatalf("unexpected call %d", call)
			return "", nil
		}
	}

	if err := CreateWorkspace("/tmp/repo", "/tmp/ws", "feature-ws", "HEAD"); err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if call != 2 {
		t.Fatalf("runGitCtx calls = %d, want 2", call)
	}
}
