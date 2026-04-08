package data

import (
	"testing"
	"time"
)

func TestNewWorkspace(t *testing.T) {
	before := time.Now()
	ws := NewWorkspace("feature-1", "feature-1", "origin/main", "/repo", "/workspaces/feature-1")
	after := time.Now()

	if ws.Name != "feature-1" {
		t.Errorf("Name = %v, want feature-1", ws.Name)
	}
	if ws.Branch != "feature-1" {
		t.Errorf("Branch = %v, want feature-1", ws.Branch)
	}
	if ws.Base != "origin/main" {
		t.Errorf("Base = %v, want origin/main", ws.Base)
	}
	if ws.Repo != "/repo" {
		t.Errorf("Repo = %v, want /repo", ws.Repo)
	}
	if ws.Root != "/workspaces/feature-1" {
		t.Errorf("Root = %v, want /workspaces/feature-1", ws.Root)
	}
	if ws.Created.Before(before) || ws.Created.After(after) {
		t.Errorf("Created time should be between test start and end")
	}
}

func TestComposeChildWorkspaceName(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		child  string
		want   string
	}{
		{name: "prefixes child", parent: "feature", child: "refactor", want: "feature.refactor"},
		{name: "keeps dotted prefix", parent: "feature", child: "feature.refactor", want: "feature.refactor"},
		{name: "keeps dashed prefix", parent: "feature", child: "feature-api", want: "feature-api"},
		{name: "empty parent", parent: "", child: "refactor", want: "refactor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComposeChildWorkspaceName(tt.parent, tt.child); got != tt.want {
				t.Fatalf("ComposeChildWorkspaceName(%q, %q) = %q, want %q", tt.parent, tt.child, got, tt.want)
			}
		})
	}
}

func TestApplyStackParent(t *testing.T) {
	parent := NewWorkspace("feature", "feature", "main", "/repo", "/repo/.amux/workspaces/feature")
	parent.StackDepth = 1
	parent.StackRootWorkspaceID = parent.ID()

	child := NewWorkspace("feature.refactor", "feature.refactor", "feature", "/repo", "/repo/.amux/workspaces/feature.refactor")
	ApplyStackParent(child, parent, "feature")

	if child.ParentWorkspaceID != parent.ID() {
		t.Fatalf("ParentWorkspaceID = %q, want %q", child.ParentWorkspaceID, parent.ID())
	}
	if child.ParentBranch != "feature" {
		t.Fatalf("ParentBranch = %q, want %q", child.ParentBranch, "feature")
	}
	if child.StackRootWorkspaceID != parent.ID() {
		t.Fatalf("StackRootWorkspaceID = %q, want %q", child.StackRootWorkspaceID, parent.ID())
	}
	if child.StackDepth != 2 {
		t.Fatalf("StackDepth = %d, want %d", child.StackDepth, 2)
	}
}

func TestFlattenWorkspaceTree(t *testing.T) {
	main := NewWorkspace("repo", "main", "main", "/repo", "/repo")
	main.Created = time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)
	feature := NewWorkspace("feature", "feature", "main", "/repo", "/repo/.amux/workspaces/feature")
	ApplyStackParent(feature, main, "main")
	child := NewWorkspace("feature.refactor", "feature.refactor", "feature", "/repo", "/repo/.amux/workspaces/feature.refactor")
	ApplyStackParent(child, feature, "feature")
	detached := NewWorkspace("lonely", "lonely", "main", "/repo", "/repo/.amux/workspaces/lonely")
	detached.Created = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	entries := FlattenWorkspaceTree([]*Workspace{feature, child, detached, main}, WorkspaceCreatedDescLess)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	gotNames := []string{
		entries[0].Workspace.Name,
		entries[1].Workspace.Name,
		entries[2].Workspace.Name,
		entries[3].Workspace.Name,
	}
	wantNames := []string{"repo", "feature", "feature.refactor", "lonely"}
	for i := range wantNames {
		if gotNames[i] != wantNames[i] {
			t.Fatalf("FlattenWorkspaceTree names = %v, want %v", gotNames, wantNames)
		}
	}
	if entries[2].Depth != 2 {
		t.Fatalf("child depth = %d, want %d", entries[2].Depth, 2)
	}
}

func TestWorkspace_ID(t *testing.T) {
	ws1 := Workspace{Repo: "/repo1", Root: "/workspaces/ws1"}
	ws2 := Workspace{Repo: "/repo1", Root: "/workspaces/ws2"}
	ws3 := Workspace{Repo: "/repo1", Root: "/workspaces/ws1"}            // Same as ws1
	ws4 := Workspace{Repo: "/repo1/../repo1", Root: "/workspaces/./ws1"} // Normalized to ws1

	id1 := ws1.ID()
	id2 := ws2.ID()
	id3 := ws3.ID()
	id4 := ws4.ID()

	if id1 == id2 {
		t.Errorf("Different workspaces should have different IDs")
	}
	if id1 != id3 {
		t.Errorf("Same workspaces should have same IDs: %v != %v", id1, id3)
	}
	if id1 != id4 {
		t.Errorf("Normalized paths should have same IDs: %v != %v", id1, id4)
	}
	if len(id1) != 16 {
		t.Errorf("ID should be 16 hex characters (8 bytes), got %d", len(id1))
	}
}

func TestWorkspace_IsPrimaryCheckout(t *testing.T) {
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
			name:    "workspace",
			repo:    "/home/user/repo",
			root:    "/home/user/.amux/workspaces/feature",
			primary: false,
		},
		{
			name:    "normalized path equivalence",
			repo:    "/home/user/repo",
			root:    "/home/user/../user/repo/.",
			primary: true,
		},
		{
			name:    "empty paths are never primary",
			repo:    "",
			root:    "",
			primary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := Workspace{Repo: tt.repo, Root: tt.root}
			if ws.IsPrimaryCheckout() != tt.primary {
				t.Errorf("IsPrimaryCheckout() = %v, want %v", ws.IsPrimaryCheckout(), tt.primary)
			}
		})
	}
}

func TestWorkspace_IsMainBranch(t *testing.T) {
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
			ws := Workspace{Branch: tt.branch}
			if ws.IsMainBranch() != tt.wantYes {
				t.Fatalf("IsMainBranch() = %v, want %v", ws.IsMainBranch(), tt.wantYes)
			}
		})
	}
}

func TestIsValidWorkspaceID(t *testing.T) {
	tests := []struct {
		name string
		id   WorkspaceID
		want bool
	}{
		{name: "valid hex id", id: WorkspaceID("0123456789abcdef"), want: true},
		{name: "too short", id: WorkspaceID("abc123"), want: false},
		{name: "uppercase", id: WorkspaceID("0123456789ABCDEF"), want: false},
		{name: "path traversal", id: WorkspaceID("../../../tmp"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidWorkspaceID(tt.id); got != tt.want {
				t.Fatalf("IsValidWorkspaceID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
