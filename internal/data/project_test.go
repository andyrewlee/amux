package data

import (
	"testing"
)

func TestNewProject(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
	}{
		{
			name:     "simple path",
			path:     "/home/user/myproject",
			wantName: "myproject",
		},
		{
			name:     "nested path",
			path:     "/home/user/code/repos/my-repo",
			wantName: "my-repo",
		},
		{
			name:     "root path",
			path:     "/",
			wantName: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProject(tt.path)
			if p.Name != tt.wantName {
				t.Errorf("NewProject().Name = %v, want %v", p.Name, tt.wantName)
			}
			if p.Path != tt.path {
				t.Errorf("NewProject().Path = %v, want %v", p.Path, tt.path)
			}
			if len(p.Worktrees) != 0 {
				t.Errorf("NewProject().Worktrees should be empty, got %d", len(p.Worktrees))
			}
		})
	}
}

func TestProject_AddWorktree(t *testing.T) {
	p := NewProject("/home/user/myproject")
	wt := Worktree{Name: "feature-1", Root: "/path/to/wt"}

	p.AddWorktree(wt)

	if len(p.Worktrees) != 1 {
		t.Errorf("Expected 1 worktree, got %d", len(p.Worktrees))
	}
	if p.Worktrees[0].Name != "feature-1" {
		t.Errorf("Expected worktree name 'feature-1', got %s", p.Worktrees[0].Name)
	}
}

func TestProject_FindWorktree(t *testing.T) {
	p := NewProject("/home/user/myproject")
	wt1 := Worktree{Name: "feature-1", Root: "/path/to/wt1"}
	wt2 := Worktree{Name: "feature-2", Root: "/path/to/wt2"}
	p.AddWorktree(wt1)
	p.AddWorktree(wt2)

	tests := []struct {
		name     string
		root     string
		wantName string
		wantNil  bool
	}{
		{"find first", "/path/to/wt1", "feature-1", false},
		{"find second", "/path/to/wt2", "feature-2", false},
		{"not found", "/path/to/wt3", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.FindWorktree(tt.root)
			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected worktree, got nil")
				} else if result.Name != tt.wantName {
					t.Errorf("Expected name %s, got %s", tt.wantName, result.Name)
				}
			}
		})
	}
}

func TestProject_FindWorktreeByName(t *testing.T) {
	p := NewProject("/home/user/myproject")
	wt1 := Worktree{Name: "feature-1", Root: "/path/to/wt1"}
	wt2 := Worktree{Name: "feature-2", Root: "/path/to/wt2"}
	p.AddWorktree(wt1)
	p.AddWorktree(wt2)

	tests := []struct {
		name     string
		findName string
		wantRoot string
		wantNil  bool
	}{
		{"find first", "feature-1", "/path/to/wt1", false},
		{"find second", "feature-2", "/path/to/wt2", false},
		{"not found", "feature-3", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.FindWorktreeByName(tt.findName)
			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected worktree, got nil")
				} else if result.Root != tt.wantRoot {
					t.Errorf("Expected root %s, got %s", tt.wantRoot, result.Root)
				}
			}
		})
	}
}
