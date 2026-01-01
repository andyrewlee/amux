package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func makeProject() data.Project {
	return data.Project{
		Name: "repo",
		Path: "/repo",
		Worktrees: []data.Worktree{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/.amux/worktrees/feature"},
		},
	}
}

func TestDashboardRebuildRowsSkipsMainAndPrimary(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	var worktreeRows int
	var projectRows int
	for _, row := range m.rows {
		switch row.Type {
		case RowWorktree:
			worktreeRows++
		case RowProject:
			projectRows++
		}
	}

	if projectRows != 1 {
		t.Fatalf("expected 1 project row, got %d", projectRows)
	}
	if worktreeRows != 1 {
		t.Fatalf("expected only non-main/non-primary worktree rows, got %d", worktreeRows)
	}
}

func TestDashboardDirtyFilter(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})

	root := project.Worktrees[1].Root
	m.statusCache[root] = &git.StatusResult{Clean: true}
	m.filterDirty = true
	m.rebuildRows()

	for _, row := range m.rows {
		if row.Type == RowWorktree {
			t.Fatalf("expected clean worktree to be hidden by dirty filter")
		}
	}

	m.statusCache[root] = &git.StatusResult{Clean: false}
	m.rebuildRows()

	found := false
	for _, row := range m.rows {
		if row.Type == RowWorktree {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dirty worktree to be visible when filter enabled")
	}
}

func TestDashboardHandleEnterProjectSelectsMain(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Row order: Home, AddProject, Project...
	m.cursor = 2
	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorktreeActivated)
	if !ok {
		t.Fatalf("expected WorktreeActivated, got %T", msg)
	}
	if activated.Worktree == nil || activated.Worktree.Branch != "main" {
		t.Fatalf("expected main worktree activation, got %+v", activated.Worktree)
	}
}
