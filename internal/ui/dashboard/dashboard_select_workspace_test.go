package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// A workspace that already has a row is selected immediately.
func TestSelectWorkspace_MovesCursorToExistingRow(t *testing.T) {
	wsList := []data.Workspace{
		*data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1"),
		*data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2"),
	}

	m := New()
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: wsList}})

	target := string(wsList[1].ID())
	m.SelectWorkspace(target)

	if got := m.selectedWorkspaceIDAt(m.cursor); got != target {
		t.Fatalf("expected cursor on %s, got %q", target, got)
	}
}

// A just-created workspace is activated before the projects reload that carries
// it lands, so the selection has to survive until its row exists.
func TestSelectWorkspace_AppliesOnceRowAppears(t *testing.T) {
	existing := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	created := *data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2")

	m := New()
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing}}})

	createdID := string(created.ID())
	m.SelectWorkspace(createdID)
	if got := m.selectedWorkspaceIDAt(m.cursor); got == createdID {
		t.Fatal("expected no selection before the workspace has a row")
	}

	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing, created}}})

	if got := m.selectedWorkspaceIDAt(m.cursor); got != createdID {
		t.Fatalf("expected cursor on the created workspace, got %q", got)
	}
	if m.pendingSelectID != "" {
		t.Fatalf("expected pending selection cleared, got %q", m.pendingSelectID)
	}
}

// Navigating by hand while a selection is still pending drops it, so a later
// reload cannot yank the cursor off the row the user chose.
func TestSelectWorkspace_UserNavigationCancelsPendingSelection(t *testing.T) {
	existing := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	created := *data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2")

	m := New()
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing}}})
	m.Focus()
	m.SelectWorkspace(string(created.ID()))

	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.pendingSelectID != "" {
		t.Fatalf("expected user navigation to drop the pending selection, got %q", m.pendingSelectID)
	}

	before := m.cursor
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing, created}}})
	if m.cursor != before {
		t.Fatalf("expected cursor to stay at %d after reload, got %d", before, m.cursor)
	}
}

// The pending selection must not override a later reload once it has been
// applied: normal cursor anchoring takes over again.
func TestSelectWorkspace_DoesNotReapplyAfterConsumption(t *testing.T) {
	first := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	second := *data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2")
	all := []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{first, second}}}

	m := New()
	m.SetProjects(all)
	m.SelectWorkspace(string(second.ID()))

	m.moveCursor(-1)
	movedTo := m.selectedWorkspaceIDAt(m.cursor)

	m.SetProjects(all)

	if got := m.selectedWorkspaceIDAt(m.cursor); got != movedTo {
		t.Fatalf("expected cursor to stay on %q after reload, got %q", movedTo, got)
	}
}
