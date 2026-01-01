package data

import (
	"testing"
	"time"
)

func TestNewWorktree(t *testing.T) {
	before := time.Now()
	wt := NewWorktree("feature-1", "feature-1", "origin/main", "/repo", "/worktrees/feature-1")
	after := time.Now()

	if wt.Name != "feature-1" {
		t.Errorf("Name = %v, want feature-1", wt.Name)
	}
	if wt.Branch != "feature-1" {
		t.Errorf("Branch = %v, want feature-1", wt.Branch)
	}
	if wt.Base != "origin/main" {
		t.Errorf("Base = %v, want origin/main", wt.Base)
	}
	if wt.Repo != "/repo" {
		t.Errorf("Repo = %v, want /repo", wt.Repo)
	}
	if wt.Root != "/worktrees/feature-1" {
		t.Errorf("Root = %v, want /worktrees/feature-1", wt.Root)
	}
	if wt.Created.Before(before) || wt.Created.After(after) {
		t.Errorf("Created time should be between test start and end")
	}
}

func TestWorktree_ID(t *testing.T) {
	wt1 := Worktree{Repo: "/repo1", Root: "/worktrees/wt1"}
	wt2 := Worktree{Repo: "/repo1", Root: "/worktrees/wt2"}
	wt3 := Worktree{Repo: "/repo1", Root: "/worktrees/wt1"} // Same as wt1

	id1 := wt1.ID()
	id2 := wt2.ID()
	id3 := wt3.ID()

	if id1 == id2 {
		t.Errorf("Different worktrees should have different IDs")
	}
	if id1 != id3 {
		t.Errorf("Same worktrees should have same IDs: %v != %v", id1, id3)
	}
	if len(id1) != 16 {
		t.Errorf("ID should be 16 hex characters (8 bytes), got %d", len(id1))
	}
}

func TestWorktree_IsPrimaryCheckout(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		root    string
		primary bool
	}{
		{
			name:    "primary checkout",
			repo:    "/home/user/repo",
			root:    "/home/user/repo",
			primary: true,
		},
		{
			name:    "worktree",
			repo:    "/home/user/repo",
			root:    "/home/user/.amux/worktrees/feature",
			primary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wt := Worktree{Repo: tt.repo, Root: tt.root}
			if wt.IsPrimaryCheckout() != tt.primary {
				t.Errorf("IsPrimaryCheckout() = %v, want %v", wt.IsPrimaryCheckout(), tt.primary)
			}
		})
	}
}

func TestWorktree_IsMainBranch(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantYes bool
	}{
		{"main", "main", true},
		{"master", "master", true},
		{"feature", "feature-1", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wt := Worktree{Branch: tt.branch}
			if wt.IsMainBranch() != tt.wantYes {
				t.Fatalf("IsMainBranch() = %v, want %v", wt.IsMainBranch(), tt.wantYes)
			}
		})
	}
}
