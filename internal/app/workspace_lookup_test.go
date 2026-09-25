package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestFindWorkspaceByID_PrefersActiveWorkspace(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	ws := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.Workspaces = append(project.Workspaces, *ws)

	// activeWorkspace is a distinct pointer with the same identity
	active := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	active.Assistant = "codex"

	app := &App{
		projects:        []data.Project{*project},
		activeWorkspace: active,
	}

	found := app.findWorkspaceByID(string(ws.ID()))
	if found == nil {
		t.Fatal("expected workspace to be found")
	}
	if found != active {
		t.Fatal("expected findWorkspaceByID to prefer activeWorkspace pointer")
	}
	if found.Assistant != "codex" {
		t.Fatalf("expected assistant %q, got %q", "codex", found.Assistant)
	}
}

func TestFindWorkspaceByID_FallsBackToProjects(t *testing.T) {
	repo := "/tmp/repo"
	root := "/tmp/workspaces/repo/feature"

	ws := data.NewWorkspace("feature", "feat-branch", "main", repo, root)
	project := data.NewProject(repo)
	project.Workspaces = append(project.Workspaces, *ws)

	app := &App{
		projects:        []data.Project{*project},
		activeWorkspace: nil, // no active workspace
	}

	found := app.findWorkspaceByID(string(ws.ID()))
	if found == nil {
		t.Fatal("expected workspace to be found via project scan")
	}
	if found.Name != "feature" {
		t.Fatalf("expected name %q, got %q", "feature", found.Name)
	}
}

// TestRootsReferToSameWorkspace_Contract pins the PATH-question semantics:
// canonical root equality with whitespace/casing tolerance, no repo leg
// (the message carries none). The stricter identity predicate lives in
// sidebar's sameWorkspaceByCanonicalPaths — see its test for the split.
func TestRootsReferToSameWorkspace_Contract(t *testing.T) {
	tmp := t.TempDir()

	for _, tt := range []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{"exact match", tmp, tmp, true},
		{"whitespace tolerated", " " + tmp + " ", tmp, true},
		{"trailing slash canonicalized", tmp + "/", tmp, true},
		{"different path", tmp, tmp + "-other", false},
		{"empty left", "", tmp, false},
		{"empty right", tmp, "", false},
		{"both empty", "", "", false},
	} {
		if got := rootsReferToSameWorkspace(tt.left, tt.right); got != tt.want {
			t.Errorf("%s: rootsReferToSameWorkspace(%q, %q) = %v, want %v", tt.name, tt.left, tt.right, got, tt.want)
		}
	}
}
