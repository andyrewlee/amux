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
		Workspaces: []data.Workspace{
			{Name: "test-project", Branch: "main", Repo: "/test-project", Root: "/test-project"},
			{Name: "feature", Branch: "feature", Repo: "/test-project", Root: "/test-project/feature"},
		},
	}

	m := New()
	m.SetProjects([]data.Project{project})

	t.Run("main branch active", func(t *testing.T) {
		// Intent: if the project's primary/main workspace has an active chat tab,
		// the project row should be marked active.
		m.activeWorkspaceIDs = map[string]bool{
			string(project.Workspaces[0].ID()): true,
		}
		if !m.isProjectActive(&project) {
			t.Errorf("expected project to be active when main workspace is active")
		}
	})

	t.Run("feature branch active", func(t *testing.T) {
		// Intent: active child workspaces should not make the project row active.
		m.activeWorkspaceIDs = map[string]bool{
			string(project.Workspaces[1].ID()): true,
		}
		if m.isProjectActive(&project) {
			t.Errorf("expected project to remain inactive when feature workspace is active")
		}
	})

	t.Run("no branch active", func(t *testing.T) {
		// Intent: if nothing is active, neither project nor workspaces are active.
		m.activeWorkspaceIDs = map[string]bool{}
		if m.isProjectActive(&project) {
			t.Errorf("expected project to NOT be active when nothing is active")
		}
	})

	t.Run("main and feature active", func(t *testing.T) {
		// Intent: if both the project workspace and a child workspace are active,
		// the project remains active (and the child workspace should still show active).
		m.activeWorkspaceIDs = map[string]bool{
			string(project.Workspaces[0].ID()): true,
			string(project.Workspaces[1].ID()): true,
		}
		if !m.isProjectActive(&project) {
			t.Errorf("expected project to be active when main workspace is active")
		}
	})
}

func TestDashboardGetMainWorkspace(t *testing.T) {
	project := data.Project{
		Workspaces: []data.Workspace{
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/feature"},
			{Name: "main-wt", Branch: "main", Repo: "/repo", Root: "/repo"},
		},
	}

	m := New()
	main := m.getMainWorkspace(&project)
	if main == nil {
		t.Fatalf("expected main workspace to be found")
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

	// Activate a workspace
	m.Update(messages.WorkspaceActivated{
		Workspace: &data.Workspace{Root: "/some/root"},
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
