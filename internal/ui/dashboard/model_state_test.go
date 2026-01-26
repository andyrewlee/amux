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
