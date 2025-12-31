package git

import (
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int
		entries []WorktreeEntry
	}{
		{
			name:   "empty",
			output: "",
			want:   0,
		},
		{
			name: "single worktree",
			output: `worktree /home/user/repo
HEAD abc123def456
branch refs/heads/main
`,
			want: 1,
			entries: []WorktreeEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: "main"},
			},
		},
		{
			name: "multiple worktrees",
			output: `worktree /home/user/repo
HEAD abc123def456
branch refs/heads/main

worktree /home/user/.amux/worktrees/feature
HEAD def456abc123
branch refs/heads/feature
`,
			want: 2,
			entries: []WorktreeEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: "main"},
				{Path: "/home/user/.amux/worktrees/feature", Head: "def456abc123", Branch: "feature"},
			},
		},
		{
			name: "bare repository",
			output: `worktree /home/user/repo.git
bare
`,
			want: 1,
			entries: []WorktreeEntry{
				{Path: "/home/user/repo.git", Bare: true},
			},
		},
		{
			name: "detached HEAD",
			output: `worktree /home/user/repo
HEAD abc123def456
detached
`,
			want: 1,
			entries: []WorktreeEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorktreeList(tt.output)

			if len(result) != tt.want {
				t.Errorf("parseWorktreeList() returned %d entries, want %d", len(result), tt.want)
				return
			}

			for i, want := range tt.entries {
				if i >= len(result) {
					break
				}
				got := result[i]
				if got.Path != want.Path {
					t.Errorf("entry[%d].Path = %v, want %v", i, got.Path, want.Path)
				}
				if got.Head != want.Head {
					t.Errorf("entry[%d].Head = %v, want %v", i, got.Head, want.Head)
				}
				if got.Branch != want.Branch {
					t.Errorf("entry[%d].Branch = %v, want %v", i, got.Branch, want.Branch)
				}
				if got.Bare != want.Bare {
					t.Errorf("entry[%d].Bare = %v, want %v", i, got.Bare, want.Bare)
				}
			}
		})
	}
}
