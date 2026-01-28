package git

import (
	"testing"
)

func TestParseWorkspaceList(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int
		entries []workspaceEntry
	}{
		{
			name:   "empty",
			output: "",
			want:   0,
		},
		{
			name: "single workspace",
			output: `worktree /home/user/repo
HEAD abc123def456
branch refs/heads/main
`,
			want: 1,
			entries: []workspaceEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: "main"},
			},
		},
		{
			name: "multiple workspaces",
			output: `worktree /home/user/repo
HEAD abc123def456
branch refs/heads/main

worktree /home/user/.amux/workspaces/feature
HEAD def456abc123
branch refs/heads/feature
`,
			want: 2,
			entries: []workspaceEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: "main"},
				{Path: "/home/user/.amux/workspaces/feature", Head: "def456abc123", Branch: "feature"},
			},
		},
		{
			name: "bare repository",
			output: `worktree /home/user/repo.git
bare
`,
			want: 1,
			entries: []workspaceEntry{
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
			entries: []workspaceEntry{
				{Path: "/home/user/repo", Head: "abc123def456", Branch: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorkspaceList(tt.output)

			if len(result) != tt.want {
				t.Errorf("parseWorkspaceList() returned %d entries, want %d", len(result), tt.want)
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
