package dashboard

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestDashboardCreatingWorktreeRow(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})

	wt := data.NewWorktree("creating", "creating", "HEAD", project.Path, project.Path+"/.amux/worktrees/creating")
	m.SetWorktreeCreating(wt, true)

	found := false
	for _, row := range m.rows {
		if row.Type == RowWorktree && row.Worktree != nil && row.Worktree.Root == wt.Root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected creating worktree to be visible in rows")
	}
}

func TestDashboardWorktreeOrderByCreatedDesc(t *testing.T) {
	m := New()
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Worktrees: []data.Worktree{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo", Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "older", Branch: "older", Repo: "/repo", Root: "/repo/.amux/worktrees/older", Created: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "newer", Branch: "newer", Repo: "/repo", Root: "/repo/.amux/worktrees/newer", Created: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}

	m.SetProjects([]data.Project{project})

	var got []string
	for _, row := range m.rows {
		if row.Type == RowWorktree {
			got = append(got, row.Worktree.Name)
		}
	}

	want := []string{"newer", "older"}
	if len(got) != len(want) {
		t.Fatalf("expected %d worktree rows, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected worktree order %v, got %v", want, got)
		}
	}
}

func TestDashboardCreatingWorktreeOrder(t *testing.T) {
	m := New()
	project := data.Project{
		Name: "repo",
		Path: "/repo",
		Worktrees: []data.Worktree{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo", Created: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "older", Branch: "older", Repo: "/repo", Root: "/repo/.amux/worktrees/older", Created: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	m.SetProjects([]data.Project{project})

	wt := data.NewWorktree("creating", "creating", "HEAD", project.Path, project.Path+"/.amux/worktrees/creating")
	wt.Created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.SetWorktreeCreating(wt, true)

	var got []string
	for _, row := range m.rows {
		if row.Type == RowWorktree {
			got = append(got, row.Worktree.Name)
		}
	}

	if len(got) == 0 || got[0] != "creating" {
		t.Fatalf("expected creating worktree to be first, got %v", got)
	}
}
