package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDashboardIsProjectActive(t *testing.T) {
	project := data.Project{
		Name: "test-project",
		Path: "/test-project",
		Worktrees: []data.Worktree{
			{Name: "test-project", Branch: "main", Repo: "/test-project", Root: "/test-project"},
			{Name: "feature", Branch: "feature", Repo: "/test-project", Root: "/test-project/feature"},
		},
	}

	m := New()
	m.SetProjects([]data.Project{project})

	t.Run("main branch active", func(t *testing.T) {
		m.activeRoot = "/test-project"
		if !m.isProjectActive(&project) {
			t.Errorf("expected project to be active when main worktree is active")
		}
	})

	t.Run("feature branch active", func(t *testing.T) {
		m.activeRoot = "/test-project/feature"
		if m.isProjectActive(&project) {
			t.Errorf("expected project to NOT be active when feature worktree is active")
		}
	})

	t.Run("no branch active", func(t *testing.T) {
		m.activeRoot = ""
		if m.isProjectActive(&project) {
			t.Errorf("expected project to NOT be active when nothing is active")
		}
	})
}

func TestDashboardGetMainWorktree(t *testing.T) {
	project := data.Project{
		Worktrees: []data.Worktree{
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/feature"},
			{Name: "main-wt", Branch: "main", Repo: "/repo", Root: "/repo"},
		},
	}

	m := New()
	main := m.getMainWorktree(&project)
	if main == nil {
		t.Fatalf("expected main worktree to be found")
	}
	if main.Branch != "main" {
		t.Errorf("expected main branch, got %s", main.Branch)
	}
}

func TestDashboardHomeActive(t *testing.T) {
	m := New()

	// Initially home is active (activeRoot is empty)
	if m.activeRoot != "" {
		t.Errorf("expected activeRoot to be empty initially")
	}

	// Activate a worktree
	m.Update(messages.WorktreeActivated{
		Worktree: &data.Worktree{Root: "/some/root"},
	})
	if m.activeRoot != "/some/root" {
		t.Errorf("expected activeRoot to be /some/root")
	}

	// Show welcome (go home)
	m.Update(messages.ShowWelcome{})
	if m.activeRoot != "" {
		t.Errorf("expected activeRoot to be empty after ShowWelcome")
	}
}
