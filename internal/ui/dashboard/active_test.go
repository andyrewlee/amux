package dashboard

import (
	"testing"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
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
		m.activeWorkspaceIDs = map[string]bool{
			string(project.Workspaces[0].ID()): true,
		}
		if !m.isProjectActive(&project) {
			t.Errorf("expected project to be active when main workspace is active")
		}
	})

	t.Run("feature branch active", func(t *testing.T) {
		m.activeWorkspaceIDs = map[string]bool{
			string(project.Workspaces[1].ID()): true,
		}
		if m.isProjectActive(&project) {
			t.Errorf("expected project to remain inactive when feature workspace is active")
		}
	})

	t.Run("no branch active", func(t *testing.T) {
		m.activeWorkspaceIDs = map[string]bool{}
		if m.isProjectActive(&project) {
			t.Errorf("expected project to NOT be active when nothing is active")
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

func TestUnreadWorkspaceTransitions(t *testing.T) {
	wsID := "ws-1"

	t.Run("tmux active to inactive marks unread", func(t *testing.T) {
		m := New()
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		newUnread := m.SetTmuxConfirmedActive(map[string]bool{})
		if !newUnread {
			t.Error("expected newUnread to be true when workspace drops out of tmuxConfirmedActive")
		}
		if !m.unreadWorkspaces[wsID] {
			t.Error("expected unreadWorkspaces to contain wsID after active→inactive")
		}
	})

	t.Run("tmux staying active does not trigger unread", func(t *testing.T) {
		m := New()
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		newUnread := m.SetTmuxConfirmedActive(map[string]bool{wsID: true})
		if newUnread {
			t.Error("expected newUnread to be false when workspace stays active")
		}
		if m.unreadWorkspaces[wsID] {
			t.Error("expected unreadWorkspaces to NOT contain wsID when still active")
		}
	})

	t.Run("tmux inactive staying inactive does not trigger unread", func(t *testing.T) {
		m := New()
		m.tmuxConfirmedActive = map[string]bool{}

		newUnread := m.SetTmuxConfirmedActive(map[string]bool{})
		if newUnread {
			t.Error("expected newUnread to be false when nothing was active")
		}
	})

	t.Run("already ready does not trigger again", func(t *testing.T) {
		m := New()
		m.tmuxConfirmedActive = map[string]bool{wsID: true}
		m.unreadWorkspaces[wsID] = true

		newUnread := m.SetTmuxConfirmedActive(map[string]bool{})
		if newUnread {
			t.Error("expected newUnread to be false when workspace is already marked unread")
		}
	})

	t.Run("MarkRead removes the flag", func(t *testing.T) {
		m := New()
		m.unreadWorkspaces[wsID] = true

		m.MarkRead(wsID)
		if m.unreadWorkspaces[wsID] {
			t.Error("expected unreadWorkspaces to NOT contain wsID after MarkRead")
		}
	})
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
