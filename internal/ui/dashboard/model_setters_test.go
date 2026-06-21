package dashboard

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestDashboardSetActiveWorkspaces(t *testing.T) {
	t.Run("stores provided map", func(t *testing.T) {
		m := New()
		active := map[string]bool{"ws-a": true, "ws-b": false}

		m.SetActiveWorkspaces(active)

		if len(m.activeWorkspaceIDs) != 2 {
			t.Fatalf("expected 2 active workspace entries, got %d", len(m.activeWorkspaceIDs))
		}
		if !m.activeWorkspaceIDs["ws-a"] {
			t.Fatalf("expected ws-a to be active")
		}
		if m.activeWorkspaceIDs["ws-b"] {
			t.Fatalf("expected ws-b to be inactive")
		}
	})

	t.Run("replaces previous set", func(t *testing.T) {
		m := New()
		m.SetActiveWorkspaces(map[string]bool{"old": true})
		m.SetActiveWorkspaces(map[string]bool{"new": true})

		if m.activeWorkspaceIDs["old"] {
			t.Fatalf("expected previous active set to be replaced, 'old' still present")
		}
		if !m.activeWorkspaceIDs["new"] {
			t.Fatalf("expected 'new' to be active after replacement")
		}
	})

	t.Run("nil map clears activity", func(t *testing.T) {
		m := New()
		m.SetActiveWorkspaces(map[string]bool{"ws-a": true})
		m.SetActiveWorkspaces(nil)

		if m.activeWorkspaceIDs != nil {
			t.Fatalf("expected activeWorkspaceIDs to be nil after clearing")
		}
		// Reading a nil map must not panic and must report inactive.
		if m.activeWorkspaceIDs["ws-a"] {
			t.Fatalf("expected nil active set to report ws-a inactive")
		}
	})

	t.Run("empty map reports inactive", func(t *testing.T) {
		m := New()
		m.SetActiveWorkspaces(map[string]bool{})

		if len(m.activeWorkspaceIDs) != 0 {
			t.Fatalf("expected empty active set, got %d entries", len(m.activeWorkspaceIDs))
		}
	})

	t.Run("drives projectRowActive observable behavior", func(t *testing.T) {
		m := New()
		main := &data.Workspace{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"}

		// No active workspaces: row must not render active.
		if m.projectRowActive("ws-id", main) {
			t.Fatalf("expected projectRowActive to be false before SetActiveWorkspaces")
		}

		m.SetActiveWorkspaces(map[string]bool{"ws-id": true})
		if !m.projectRowActive("ws-id", main) {
			t.Fatalf("expected projectRowActive to be true after marking ws-id active")
		}

		// A deleting main workspace supersedes active styling.
		m.deletingWorkspaces[main.Root] = true
		if m.projectRowActive("ws-id", main) {
			t.Fatalf("expected projectRowActive to be false while main workspace is deleting")
		}
	})

	t.Run("drives isProjectActive via main workspace id", func(t *testing.T) {
		m := New()
		project := makeProject()
		m.SetProjects([]data.Project{project})

		main := m.getMainWorkspace(&project)
		if main == nil {
			t.Fatalf("expected a resolvable main workspace for fixture project")
		}
		id := string(main.ID())

		if m.isProjectActive(&project) {
			t.Fatalf("expected project inactive before SetActiveWorkspaces")
		}
		m.SetActiveWorkspaces(map[string]bool{id: true})
		if !m.isProjectActive(&project) {
			t.Fatalf("expected project active after marking its main workspace id %q active", id)
		}
	})
}

func TestDashboardSetCanFocusRight(t *testing.T) {
	tests := []struct {
		name string
		set  bool
	}{
		{name: "enable", set: true},
		{name: "disable", set: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetCanFocusRight(tt.set)
			if m.canFocusRight != tt.set {
				t.Fatalf("expected canFocusRight=%v, got %v", tt.set, m.canFocusRight)
			}
		})
	}

	t.Run("toggles idempotently", func(t *testing.T) {
		m := New()
		m.SetCanFocusRight(true)
		m.SetCanFocusRight(true)
		if !m.canFocusRight {
			t.Fatalf("expected canFocusRight to remain true after repeated enable")
		}
		m.SetCanFocusRight(false)
		if m.canFocusRight {
			t.Fatalf("expected canFocusRight to be false after disable")
		}
	})
}

func TestDashboardSetShowKeymapHints(t *testing.T) {
	t.Run("stores flag", func(t *testing.T) {
		m := New()
		m.SetShowKeymapHints(true)
		if !m.showKeymapHints {
			t.Fatalf("expected showKeymapHints to be true")
		}
		m.SetShowKeymapHints(false)
		if m.showKeymapHints {
			t.Fatalf("expected showKeymapHints to be false")
		}
	})

	t.Run("gates help line count", func(t *testing.T) {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.SetSize(80, 40)

		m.SetShowKeymapHints(false)
		if got := m.helpLineCount(); got != 0 {
			t.Fatalf("expected 0 help lines when hints hidden, got %d", got)
		}

		m.SetShowKeymapHints(true)
		if got := m.helpLineCount(); got <= 0 {
			t.Fatalf("expected at least one help line when hints shown, got %d", got)
		}
	})
}

func TestDashboardSetAgentStates(t *testing.T) {
	t.Run("stores provided map", func(t *testing.T) {
		m := New()
		states := map[string]activity.AgentState{"ws-a": activity.StateWorking, "ws-b": activity.StateDone}
		m.SetAgentStates(states)
		if len(m.agentStates) != 2 {
			t.Fatalf("expected 2 agent state entries, got %d", len(m.agentStates))
		}
		if m.agentStates["ws-a"] != activity.StateWorking {
			t.Fatalf("expected ws-a to be StateWorking")
		}
		if m.agentStates["ws-b"] != activity.StateDone {
			t.Fatalf("expected ws-b to be StateDone")
		}
	})

	t.Run("nil map clears states without panic", func(t *testing.T) {
		m := New()
		m.SetAgentStates(map[string]activity.AgentState{"ws-a": activity.StateDone})
		m.SetAgentStates(nil)
		if m.agentStates != nil {
			t.Fatalf("expected agentStates to be nil after clearing")
		}
		// Reading a nil map must not panic.
		if m.agentStates["ws-a"] != activity.StateIdle {
			t.Fatalf("expected nil map to return zero value (StateIdle) for any key")
		}
	})

	t.Run("replaces previous states", func(t *testing.T) {
		m := New()
		m.SetAgentStates(map[string]activity.AgentState{"old": activity.StateWorking})
		m.SetAgentStates(map[string]activity.AgentState{"new": activity.StateDone})
		if _, ok := m.agentStates["old"]; ok {
			t.Fatalf("expected previous states to be replaced, 'old' still present")
		}
		if m.agentStates["new"] != activity.StateDone {
			t.Fatalf("expected 'new' to be StateDone after replacement")
		}
	})
}

// TestDashboardDoneRender verifies that a workspace row with StateDone renders a
// "done" status text and does NOT trigger the working style.
func TestDashboardDoneRender(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})
	m.SetSize(80, 40)

	// Find the workspace row for "feature" and get its ActivityWorkspaceID.
	var wsRow *Row
	for i := range m.rows {
		if m.rows[i].Type == RowWorkspace {
			wsRow = &m.rows[i]
			break
		}
	}
	if wsRow == nil {
		t.Fatal("expected at least one workspace row")
	}
	wsID := wsRow.ActivityWorkspaceID

	t.Run("done state renders done text", func(t *testing.T) {
		m.SetActiveWorkspaces(map[string]bool{})
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})
		rendered := m.renderRow(*wsRow, false)
		if !strings.Contains(rendered, "done") {
			t.Fatalf("expected rendered row to contain 'done', got %q", rendered)
		}
	})

	t.Run("working state does not render done text", func(t *testing.T) {
		m.SetActiveWorkspaces(map[string]bool{wsID: true})
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})
		rendered := m.renderRow(*wsRow, false)
		// When working, the "done" status text should not appear.
		if strings.Contains(rendered, " done") {
			t.Fatalf("working workspace must not render 'done' text, got %q", rendered)
		}
	})

	t.Run("idle state does not render done text", func(t *testing.T) {
		m.SetActiveWorkspaces(map[string]bool{})
		m.SetAgentStates(map[string]activity.AgentState{})
		rendered := m.renderRow(*wsRow, false)
		if strings.Contains(rendered, " done") {
			t.Fatalf("idle workspace must not render 'done' text, got %q", rendered)
		}
	})
}

func TestDashboardSetStyles(t *testing.T) {
	t.Run("replaces stored styles", func(t *testing.T) {
		m := New()

		// Build a clearly distinguishable styles value derived from the
		// defaults so the assertion proves the field was actually swapped.
		custom := common.DefaultStyles()
		custom.HomeRow = custom.HomeRow.Foreground(lipgloss.Color("#ABCDEF")).Bold(true)

		m.SetStyles(custom)

		if got := m.styles.HomeRow.GetForeground(); got != lipgloss.Color("#ABCDEF") {
			t.Fatalf("expected HomeRow foreground #ABCDEF after SetStyles, got %v", got)
		}
		if !m.styles.HomeRow.GetBold() {
			t.Fatalf("expected HomeRow bold attribute to be applied after SetStyles")
		}
	})

	t.Run("overwrites multiple style fields", func(t *testing.T) {
		m := New()
		custom := common.DefaultStyles()
		custom.WorkspaceRow = custom.WorkspaceRow.Foreground(lipgloss.Color("#112233"))
		custom.SelectedRow = custom.SelectedRow.Background(lipgloss.Color("#445566"))

		m.SetStyles(custom)

		if got := m.styles.WorkspaceRow.GetForeground(); got != lipgloss.Color("#112233") {
			t.Fatalf("expected WorkspaceRow foreground #112233, got %v", got)
		}
		if got := m.styles.SelectedRow.GetBackground(); got != lipgloss.Color("#445566") {
			t.Fatalf("expected SelectedRow background #445566, got %v", got)
		}
	})
}

// TestDashboardDoneAck verifies the "done until seen" ack state machine.
func TestDashboardDoneAck(t *testing.T) {
	// Helper: find the first RowWorkspace in m.rows and return its index and
	// ActivityWorkspaceID. Skips the project's main workspace (RowProject row).
	findWSRow := func(m *Model) (idx int, wsID string) {
		for i, row := range m.rows {
			if row.Type == RowWorkspace {
				return i, row.ActivityWorkspaceID
			}
		}
		return -1, ""
	}

	t.Run("done state renders done before ack", func(t *testing.T) {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.SetSize(80, 40)

		idx, wsID := findWSRow(m)
		if idx < 0 {
			t.Fatal("expected at least one workspace row")
		}

		m.SetActiveWorkspaces(map[string]bool{})
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})

		rendered := m.renderRow(m.rows[idx], false)
		if !strings.Contains(rendered, "done") {
			t.Fatalf("expected rendered row to contain 'done' before ack, got %q", rendered)
		}
	})

	t.Run("done indicator clears after activateCurrentRow", func(t *testing.T) {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.SetSize(80, 40)

		idx, wsID := findWSRow(m)
		if idx < 0 {
			t.Fatal("expected at least one workspace row")
		}

		m.SetActiveWorkspaces(map[string]bool{})
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})

		// Simulate user navigating to the workspace row — this is the "seen" event.
		m.cursor = idx
		_ = m.activateCurrentRow()

		rendered := m.renderRow(m.rows[idx], false)
		if strings.Contains(rendered, " done") {
			t.Fatalf("expected 'done' to be hidden after ack via activateCurrentRow, got %q", rendered)
		}
	})

	t.Run("reset-on-working cycle makes done visible again", func(t *testing.T) {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.SetSize(80, 40)

		idx, wsID := findWSRow(m)
		if idx < 0 {
			t.Fatal("expected at least one workspace row")
		}

		m.SetActiveWorkspaces(map[string]bool{})

		// Mark done and ack it.
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})
		m.cursor = idx
		_ = m.activateCurrentRow()

		// Confirm acked — not rendering "done".
		if strings.Contains(m.renderRow(m.rows[idx], false), " done") {
			t.Fatalf("expected 'done' hidden after ack")
		}

		// A new work cycle starts — SetAgentStates with StateWorking must clear the ack.
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateWorking})
		if m.doneAcked[wsID] {
			t.Fatalf("expected doneAcked to be cleared when workspace transitions to StateWorking")
		}

		// Workspace finishes again — back to StateDone (not active).
		m.SetActiveWorkspaces(map[string]bool{})
		m.SetAgentStates(map[string]activity.AgentState{wsID: activity.StateDone})

		rendered := m.renderRow(m.rows[idx], false)
		if !strings.Contains(rendered, "done") {
			t.Fatalf("expected 'done' to render again after reset-on-working cycle, got %q", rendered)
		}
	})
}

func TestDashboardInit(t *testing.T) {
	t.Run("returns nil command", func(t *testing.T) {
		m := New()
		if cmd := m.Init(); cmd != nil {
			t.Fatalf("expected Init to return a nil command, got %T", cmd)
		}
	})

	t.Run("does not mutate observable state", func(t *testing.T) {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.cursor = 2
		m.SetCanFocusRight(true)
		m.SetShowKeymapHints(true)

		rowsBefore := len(m.rows)
		cursorBefore := m.cursor

		_ = m.Init()

		if len(m.rows) != rowsBefore {
			t.Fatalf("expected Init to leave rows unchanged (%d), got %d", rowsBefore, len(m.rows))
		}
		if m.cursor != cursorBefore {
			t.Fatalf("expected Init to leave cursor unchanged (%d), got %d", cursorBefore, m.cursor)
		}
		if !m.canFocusRight {
			t.Fatalf("expected Init to leave canFocusRight unchanged")
		}
		if !m.showKeymapHints {
			t.Fatalf("expected Init to leave showKeymapHints unchanged")
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		m := New()
		for i := 0; i < 3; i++ {
			if cmd := m.Init(); cmd != nil {
				t.Fatalf("expected Init call %d to return nil, got %T", i, cmd)
			}
		}
	})
}
