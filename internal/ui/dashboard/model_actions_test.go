package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestDashboardHandleEnterProjectSelectsMain(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Row order: Home, Spacer, Project...
	m.cursor = 2
	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorkspaceActivated)
	if !ok {
		t.Fatalf("expected WorkspaceActivated, got %T", msg)
	}
	if activated.Workspace == nil || activated.Workspace.Branch != "main" {
		t.Fatalf("expected main workspace activation, got %+v", activated.Workspace)
	}
}

func TestDashboardHandleEnterHome(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 0 // Home row

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowWelcome); !ok {
		t.Fatalf("expected ShowWelcome message, got %T", msg)
	}
}

func TestDashboardHandleEnterWorkspace(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a workspace row
	for i, row := range m.rows {
		if row.Type == RowWorkspace {
			m.cursor = i
			break
		}
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorkspaceActivated)
	if !ok {
		t.Fatalf("expected WorkspaceActivated message, got %T", msg)
	}
	if activated.Workspace == nil {
		t.Fatalf("expected workspace in activation message")
	}
}

func TestDashboardHandleEnterCreate(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a create row
	for i, row := range m.rows {
		if row.Type == RowCreate {
			m.cursor = i
			break
		}
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowCreateWorkspaceDialog); !ok {
		t.Fatalf("expected ShowCreateWorkspaceDialog message, got %T", msg)
	}
}

func TestDashboardActivateCurrentRowProject(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Row order: Home, Spacer, Project...
	m.cursor = 2
	cmd := m.activateCurrentRow()
	if cmd == nil {
		t.Fatalf("expected activateCurrentRow to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorkspaceActivated)
	if !ok {
		t.Fatalf("expected WorkspaceActivated, got %T", msg)
	}
	if activated.Workspace == nil || activated.Workspace.Branch != "main" {
		t.Fatalf("expected main workspace activation, got %+v", activated.Workspace)
	}
}

func TestDashboardActivateCurrentRowWorkspace(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a workspace row
	for i, row := range m.rows {
		if row.Type == RowWorkspace {
			m.cursor = i
			break
		}
	}

	cmd := m.activateCurrentRow()
	if cmd == nil {
		t.Fatalf("expected activateCurrentRow to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorkspaceActivated)
	if !ok {
		t.Fatalf("expected WorkspaceActivated, got %T", msg)
	}
	if activated.Workspace == nil {
		t.Fatalf("expected workspace in activation message")
	}
}

func TestDashboardActivateCurrentRowHome(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 0 // Home row

	cmd := m.activateCurrentRow()
	if cmd == nil {
		t.Fatalf("expected activateCurrentRow to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowWelcome); !ok {
		t.Fatalf("expected ShowWelcome message, got %T", msg)
	}
}

func TestDashboardActivateCurrentRowCreate(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a create row
	for i, row := range m.rows {
		if row.Type == RowCreate {
			m.cursor = i
			break
		}
	}

	cmd := m.activateCurrentRow()
	if cmd != nil {
		t.Fatalf("expected activateCurrentRow to return nil for RowCreate, got a command")
	}
}

func TestDashboardArrowKeyActivatesWorkspace(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.Focus()
	m.cursor = 0 // Start at Home

	// Simulate pressing 'j' (down arrow) to move to the project row
	msg := tea.KeyPressMsg{Code: 'j', Text: "j"}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatalf("expected command from arrow key movement")
	}

	result := cmd()
	if _, ok := result.(messages.WorkspaceActivated); !ok {
		t.Fatalf("expected arrow key movement to emit WorkspaceActivated, got %T", result)
	}
}

func TestDashboardHandleDelete(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a workspace row
	for i, row := range m.rows {
		if row.Type == RowWorkspace {
			m.cursor = i
			break
		}
	}

	cmd := m.handleDelete()
	if cmd == nil {
		t.Fatalf("expected handleDelete to return a command")
	}

	msg := cmd()
	dialog, ok := msg.(messages.ShowDeleteWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowDeleteWorkspaceDialog message, got %T", msg)
	}
	if dialog.Workspace == nil {
		t.Fatalf("expected workspace in dialog message")
	}
}

func TestDashboardHandleRemoveProject(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a project row
	for i, row := range m.rows {
		if row.Type == RowProject {
			m.cursor = i
			break
		}
	}

	cmd := m.handleDelete()
	if cmd == nil {
		t.Fatalf("expected handleDelete to return a command")
	}

	msg := cmd()
	dialog, ok := msg.(messages.ShowRemoveProjectDialog)
	if !ok {
		t.Fatalf("expected ShowRemoveProjectDialog message, got %T", msg)
	}
	if dialog.Project == nil {
		t.Fatalf("expected project in dialog message")
	}
}

func TestDashboardHandleDeleteNonWorkspace(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 0 // Home row

	cmd := m.handleDelete()
	if cmd != nil {
		t.Fatalf("expected handleDelete to return nil for non-workspace row")
	}
}

func TestDashboardDeleteKeyBinding(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.Focus()

	// Find a workspace row
	for i, row := range m.rows {
		if row.Type == RowWorkspace {
			m.cursor = i
			break
		}
	}

	t.Run("lowercase d ignored", func(t *testing.T) {
		// tea.KeyPressMsg for 'd'
		msg := tea.KeyPressMsg{Code: 'd', Text: "d"}
		_, cmd := m.Update(msg)
		if cmd != nil {
			t.Fatalf("expected no command for lowercase 'd'")
		}
	})

	t.Run("uppercase D triggers delete", func(t *testing.T) {
		// tea.KeyPressMsg for 'D'
		msg := tea.KeyPressMsg{Code: 'D', Text: "D"}
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatalf("expected command for uppercase 'D'")
		}

		// Verify it's the right command
		res := cmd()
		if _, ok := res.(messages.ShowDeleteWorkspaceDialog); !ok {
			t.Fatalf("expected ShowDeleteWorkspaceDialog message, got %T", res)
		}
	})

	t.Run("uppercase D triggers remove on project", func(t *testing.T) {
		// Find a project row
		for i, row := range m.rows {
			if row.Type == RowProject {
				m.cursor = i
				break
			}
		}

		msg := tea.KeyPressMsg{Code: 'D', Text: "D"}
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatalf("expected command for uppercase 'D'")
		}

		res := cmd()
		if _, ok := res.(messages.ShowRemoveProjectDialog); !ok {
			t.Fatalf("expected ShowRemoveProjectDialog message, got %T", res)
		}
	})
}

func TestDashboardNewKeyBinding(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.Focus()

	t.Run("n key ignored", func(t *testing.T) {
		// tea.KeyPressMsg for 'n'
		msg := tea.KeyPressMsg{Code: 'n', Text: "n"}
		_, cmd := m.Update(msg)
		if cmd != nil {
			t.Fatalf("expected no command for 'n'")
		}
	})
}
