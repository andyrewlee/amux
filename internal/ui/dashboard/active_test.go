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

func TestReadyWorkspaceTransitions(t *testing.T) {
	wsID := "ws-1"

	t.Run("active to running marks ready when tmux confirmed", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 2}
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		// Transition to running (state 1) — should mark as ready because tmux confirms activity
		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 1})
		if !newReady {
			t.Error("expected newReady=true when transitioning from active(2) to running(1) with tmux confirmation")
		}
		if !m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to contain wsID after active→running transition")
		}
	})

	t.Run("active to idle marks ready when tmux confirmed", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 2}
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 0})
		if !newReady {
			t.Error("expected newReady=true when transitioning from active(2) to idle(0) with tmux confirmation")
		}
		if !m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to contain wsID after active→idle transition")
		}
	})

	t.Run("active to running without tmux confirmation does NOT mark ready", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 2}
		// tmuxConfirmedActive is empty — this is a noise event (tmux redraw)

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 1})
		if newReady {
			t.Error("expected newReady=false when tmux does not confirm activity (noise event)")
		}
		if m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to NOT contain wsID without tmux confirmation")
		}
	})

	t.Run("running to idle does not mark ready", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 1}
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 0})
		if newReady {
			t.Error("expected newReady=false when transitioning from running(1) to idle(0)")
		}
		if m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to NOT contain wsID after running→idle transition")
		}
	})

	t.Run("staying active does not trigger ready", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 2}
		m.tmuxConfirmedActive = map[string]bool{wsID: true}

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 2})
		if newReady {
			t.Error("expected newReady=false when state stays at active(2)")
		}
		if m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to NOT contain wsID when state stays active")
		}
	})

	t.Run("already ready does not trigger again", func(t *testing.T) {
		m := New()
		m.workspaceAgentStates = map[string]int{wsID: 2}
		m.tmuxConfirmedActive = map[string]bool{wsID: true}
		m.readyWorkspaces[wsID] = true // already marked ready

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{wsID: 1})
		if newReady {
			t.Error("expected newReady=false when workspace is already marked ready (no double alert)")
		}
	})

	t.Run("active to idle does not mark ready when workspace is currently viewed", func(t *testing.T) {
		m := New()
		ws := data.Workspace{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/feature"}
		project := data.Project{
			Name: "test",
			Path: "/repo",
			Workspaces: []data.Workspace{
				{Name: "main", Branch: "main", Repo: "/repo", Root: "/repo"},
				ws,
			},
		}
		m.SetProjects([]data.Project{project})

		// Move cursor to the workspace row
		for i, row := range m.rows {
			if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Name == "feature" {
				m.cursor = i
				break
			}
		}

		viewedID := string(ws.ID())
		m.workspaceAgentStates = map[string]int{viewedID: 2}
		m.tmuxConfirmedActive = map[string]bool{viewedID: true}

		_, newReady := m.SetWorkspaceAgentStates(map[string]int{viewedID: 0})
		if newReady {
			t.Error("expected newReady=false when workspace is currently viewed")
		}
		if m.readyWorkspaces[viewedID] {
			t.Error("expected readyWorkspaces to NOT contain viewed workspace")
		}
	})

	t.Run("ClearReady removes the flag", func(t *testing.T) {
		m := New()
		m.readyWorkspaces[wsID] = true

		m.ClearReady(wsID)
		if m.readyWorkspaces[wsID] {
			t.Error("expected readyWorkspaces to NOT contain wsID after ClearReady")
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
