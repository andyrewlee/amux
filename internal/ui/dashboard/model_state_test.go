package dashboard

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestDashboardCreatingWorkspaceRow(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})

	wt := data.NewWorkspace("creating", "creating", "HEAD", project.Path, project.Path+"/.amux/workspaces/creating")
	m.SetWorkspaceCreating(wt, true)

	found := false
	for _, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root == wt.Root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected creating workspace to be visible in rows")
	}
}

func TestDashboardWorkspaceOrderByCreatedDesc(t *testing.T) {
	m := New()
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo", Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "older", Branch: "older", Repo: "/repo", Root: "/repo/.amux/workspaces/older", Created: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "newer", Branch: "newer", Repo: "/repo", Root: "/repo/.amux/workspaces/newer", Created: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	m.SetProjects([]data.Project{project})

	var got []string
	for _, row := range m.rows {
		if row.Type == RowWorkspace {
			got = append(got, row.Workspace.Name)
		}
	}

	want := []string{"newer", "older"}
	if len(got) != len(want) {
		t.Fatalf("expected %d workspace rows, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected workspace order %v, got %v", want, got)
		}
	}
}

func TestDashboardCreatingWorkspaceOrder(t *testing.T) {
	m := New()
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo", Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "older", Branch: "older", Repo: "/repo", Root: "/repo/.amux/workspaces/older", Created: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	m.SetProjects([]data.Project{project})

	wt := data.NewWorkspace("creating", "creating", "HEAD", project.Path, project.Path+"/.amux/workspaces/creating")
	wt.Created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.SetWorkspaceCreating(wt, true)

	var got []string
	for _, row := range m.rows {
		if row.Type == RowWorkspace {
			got = append(got, row.Workspace.Name)
		}
	}

	if len(got) == 0 || got[0] != "creating" {
		t.Fatalf("expected creating workspace to be first, got %v", got)
	}
}

func TestDashboardWorkspaceOrderStableWhenCreatedEqual(t *testing.T) {
	m := New()
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo", Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "b", Branch: "b", Repo: "/repo", Root: "/repo/.amux/workspaces/b", Created: created},
			{Name: "a", Branch: "a", Repo: "/repo", Root: "/repo/.amux/workspaces/a", Created: created},
			{Name: "a", Branch: "a2", Repo: "/repo", Root: "/repo/.amux/workspaces/a2", Created: created},
		},
	}

	m.SetProjects([]data.Project{project})

	var got []string
	for _, row := range m.rows {
		if row.Type == RowWorkspace {
			got = append(got, row.Workspace.Root)
		}
	}

	want := []string{
		"/repo/.amux/workspaces/a",
		"/repo/.amux/workspaces/a2",
		"/repo/.amux/workspaces/b",
	}

	if len(got) < len(want) {
		t.Fatalf("expected at least %d workspace rows, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected stable order %v, got %v", want, got[:len(want)])
		}
	}
}

func TestDashboardWorkspaceTreePlacesChildrenAfterParent(t *testing.T) {
	m := New()
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			*data.NewWorkspace("repo", "main", "main", "/repo", "/repo"),
			*data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/.amux/workspaces/feature"),
			*data.NewWorkspace("feature.refactor", "feature.refactor", "feature", "/repo", "/repo/.amux/workspaces/feature.refactor"),
			*data.NewWorkspace("other", "other", "main", "/repo", "/repo/.amux/workspaces/other"),
		},
	}
	parent := &project.Workspaces[1]
	child := &project.Workspaces[2]
	data.ApplyStackParent(parent, &project.Workspaces[0], "main")
	data.ApplyStackParent(child, parent, "feature")

	m.SetProjects([]data.Project{project})

	var got []string
	var depths []int
	for _, row := range m.rows {
		if row.Type != RowWorkspace || row.Workspace == nil {
			continue
		}
		got = append(got, row.Workspace.Name)
		depths = append(depths, row.TreeDepth)
	}

	want := []string{"feature", "feature.refactor", "other"}
	if len(got) != len(want) {
		t.Fatalf("workspace rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("workspace rows = %v, want %v", got, want)
		}
	}
	if depths[0] != 1 || depths[1] != 2 || depths[2] != 0 {
		t.Fatalf("tree depths = %v, want [1 2 0]", depths)
	}
}
