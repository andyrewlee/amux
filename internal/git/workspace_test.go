package git

import (
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
