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

	// Start below the first selectable row so 'k' demonstrably moves the cursor:
	// pressing it on the top row would prove only "some key was handled".
	m.moveCursor(1)
	moved := m.cursor
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.cursor == moved {
		t.Fatal("expected 'k' to move the cursor; test would not prove navigation clears the selection")
	}
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

// Mouse input clears a pending selection too. Only the keyboard path was
// covered, so dropping either mouse clear was silent.
func TestSelectWorkspace_MouseInputCancelsPendingSelection(t *testing.T) {
	existing := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	created := *data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2")
	projects := []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing}}}

	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"wheel", tea.MouseWheelMsg{Button: tea.MouseWheelDown}},
		{"click", tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			m.SetSize(80, 24)
			m.SetProjects(projects)
			m.Focus()
			m.SelectWorkspace(string(created.ID()))

			m.Update(tc.msg)

			if m.pendingSelectID != "" {
				t.Fatalf("expected %s input to drop the pending selection, got %q", tc.name, m.pendingSelectID)
			}
		})
	}
}

// A pending selection whose row never appears (created-with-warning, or deleted
// before its reload landed) must not linger. Workspace IDs are a hash of
// project+name, so a later delete-then-recreate at the same name reproduces the
// ID and a stranded selection would yank the cursor away long afterwards. The
// dashboard cannot rely on input to clear it: this flow parks focus on the
// center pane, and every input-side clear is gated on being focused.
func TestSelectWorkspace_ExpiresWhenRowNeverArrives(t *testing.T) {
	existing := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	ghost := *data.NewWorkspace("ghost", "ghost", "main", "/repo", "/repo/ghost")
	projects := []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing}}}

	m := New()
	m.SetProjects(projects)
	m.Blur() // this flow leaves the dashboard unfocused
	m.SelectWorkspace(string(ghost.ID()))

	for i := 0; i < pendingSelectMaxLoads; i++ {
		if m.pendingSelectID == "" {
			t.Fatalf("selection expired after %d reloads, before its row could arrive", i)
		}
		m.SetProjects(projects)
	}

	if m.pendingSelectID != "" {
		t.Fatalf("expected the pending selection to expire after %d reloads, got %q",
			pendingSelectMaxLoads, m.pendingSelectID)
	}

	// The recreated workspace must not inherit the abandoned selection.
	withGhost := []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{existing, ghost}}}
	m.cursor = 0
	m.SetProjects(withGhost)
	if got := m.selectedWorkspaceIDAt(m.cursor); got == string(ghost.ID()) {
		t.Fatal("expected an expired selection not to grab the cursor when the row later appears")
	}
}

// Shelve removes the live row immediately but only re-renders it as a shelved
// row on the next projects load. The cursor must re-anchor to that shelved
// row — without it, Enter/D after a shelve land on the predecessor (project)
// row, activating or offering to remove the project instead of restoring or
// purging the shelved workspace.
func TestSelectWorkspace_ShelveReanchorsCursorToShelvedRow(t *testing.T) {
	ws := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	wsID := string(ws.ID())
	project := data.Project{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{ws}}

	m := New()
	m.SetProjects([]data.Project{project})
	m.SelectWorkspace(wsID)

	// Transient state: the live row is gone but the reload carrying the
	// shelved row has not landed yet.
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo"}})
	if got := m.selectedWorkspaceIDAt(m.cursor); got == wsID {
		t.Fatal("expected the shelved row to be absent in the transient rebuild")
	}

	// The reload surfaces the workspace as shelved; the cursor must follow.
	shelved := ws
	shelved.Archived = true
	shelved.Shelved = true
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", ShelvedWorkspaces: []data.Workspace{shelved}}})
	if got := m.selectedWorkspaceIDAt(m.cursor); got != wsID {
		t.Fatalf("expected cursor re-anchored to the shelved row, got %q", got)
	}
	if m.rows[m.cursor].Type != RowShelved {
		t.Fatalf("expected cursor on a shelved row, got type %v", m.rows[m.cursor].Type)
	}
}

// A workspace that never reappears (delete) still walks the cursor to the
// predecessor and drops the pending re-anchor within the bounded wait.
func TestSelectWorkspace_DeleteKeepsPredecessorAnchor(t *testing.T) {
	ws := *data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1")
	project := data.Project{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{ws}}

	m := New()
	m.SetProjects([]data.Project{project})
	m.SelectWorkspace(string(ws.ID()))

	empty := []data.Project{{Name: "repo", Path: "/repo"}}
	m.SetProjects(empty)
	if m.rows[m.cursor].Type != RowProject {
		t.Fatalf("expected cursor on the predecessor row after delete, got type %v", m.rows[m.cursor].Type)
	}
	for i := 0; i < pendingSelectMaxLoads+1; i++ {
		m.SetProjects(empty)
	}
	if m.pendingSelectID != "" {
		t.Fatalf("expected pending selection to expire for a workspace that never returns, got %q", m.pendingSelectID)
	}
	if m.rows[m.cursor].Type != RowProject {
		t.Fatalf("expected cursor to stay on the predecessor row, got type %v", m.rows[m.cursor].Type)
	}
}
