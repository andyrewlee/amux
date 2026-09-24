package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func makeProjectWithShelved() data.Project {
	p := makeProject()
	p.ShelvedWorkspaces = []data.Workspace{
		{Name: "shelved-ws", Branch: "shelved", Repo: "/repo", Root: "/repo/.amux/workspaces/shelved", Archived: true, Shelved: true},
	}
	return p
}

func findRow(m *Model, rowType RowType) int {
	for i, row := range m.rows {
		if row.Type == rowType {
			return i
		}
	}
	return -1
}

func TestDashboardShelvedRowsBuiltSeparately(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithShelved()})

	shelved := 0
	workspaces := 0
	for _, row := range m.rows {
		switch row.Type {
		case RowShelved:
			shelved++
			if row.Workspace == nil || row.Workspace.Name != "shelved-ws" {
				t.Fatalf("shelved row workspace = %+v", row.Workspace)
			}
		case RowWorkspace:
			workspaces++
		}
	}
	if shelved != 1 {
		t.Fatalf("expected 1 shelved row, got %d", shelved)
	}
	if workspaces != 1 {
		t.Fatalf("shelved workspace leaked into live rows: got %d workspace rows", workspaces)
	}
}

func TestDashboardEnterOnShelvedRowRestores(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithShelved()})
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatal("expected handleEnter to return a command for a shelved row")
	}
	msg := cmd()
	restore, ok := msg.(messages.RestoreWorkspace)
	if !ok {
		t.Fatalf("expected RestoreWorkspace, got %T", msg)
	}
	if restore.Workspace == nil || restore.Workspace.Name != "shelved-ws" {
		t.Fatalf("restore workspace = %+v", restore.Workspace)
	}
}

func TestDashboardDeleteOnShelvedRowIsPurge(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithShelved()})
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}

	cmd := m.handleDelete()
	if cmd == nil {
		t.Fatal("expected handleDelete to return a command for a shelved row")
	}
	msg := cmd()
	dlg, ok := msg.(messages.ShowDeleteWorkspaceDialog)
	if !ok {
		t.Fatalf("purge must route through the delete dialog, got %T", msg)
	}
	if dlg.Workspace == nil || dlg.Workspace.Name != "shelved-ws" {
		t.Fatalf("purge workspace = %+v", dlg.Workspace)
	}
}

func TestDashboardShelveKeyEmitsDialogOnLiveRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.Focus()
	m.cursor = findRow(m, RowWorkspace)
	if m.cursor < 0 {
		t.Fatal("no workspace row")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("expected command for uppercase 'S' on a workspace row")
	}
	if _, ok := cmd().(messages.ShowShelveWorkspaceDialog); !ok {
		t.Fatalf("expected ShowShelveWorkspaceDialog, got %T", cmd())
	}
}

func TestDashboardShelveKeyIgnoredOnShelvedRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithShelved()})
	m.Focus()
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd != nil {
		t.Fatalf("expected no command for 'S' on a shelved row, got %T", cmd())
	}
}

func TestDashboardEnterKeyOnShelvedRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithShelved()})
	m.Focus()
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command for Enter on a shelved row")
	}
	if _, ok := cmd().(messages.RestoreWorkspace); !ok {
		t.Fatalf("expected RestoreWorkspace, got %T", cmd())
	}
}

func makeProjectWithTwoShelved() data.Project {
	p := makeProject()
	p.ShelvedWorkspaces = []data.Workspace{
		{Name: "shelved-a", Branch: "a", Repo: "/repo", Root: "/repo/.amux/workspaces/a", Archived: true, Shelved: true},
		{Name: "shelved-b", Branch: "b", Repo: "/repo", Root: "/repo/.amux/workspaces/b", Archived: true, Shelved: true},
	}
	return p
}

// TestDashboardEnterOnShelvedRowWithMarksEmitsBulkRestore proves marked
// shelved rows upgrade Enter to the bulk-restore dialog and that the
// items are the marked shelved rows — not the cursor row alone.
func TestDashboardEnterOnShelvedRowWithMarksEmitsBulkRestore(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithTwoShelved()})
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}
	for _, row := range m.rows {
		if row.Type == RowShelved {
			m.MarkWorkspaceIDs([]string{string(row.Workspace.MetadataID())})
		}
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatal("expected bulk restore command")
	}
	msg, ok := cmd().(messages.ShowBulkRestoreWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowBulkRestoreWorkspaceDialog, got %T", cmd())
	}
	if len(msg.Items) != 2 {
		t.Fatalf("bulk items = %d, want 2", len(msg.Items))
	}
}

// TestDashboardDeleteOnShelvedRowWithMarksEmitsBulkPurge proves marked
// shelved rows upgrade D to the typed bulk-purge dialog.
func TestDashboardDeleteOnShelvedRowWithMarksEmitsBulkPurge(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithTwoShelved()})
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}
	for _, row := range m.rows {
		if row.Type == RowShelved {
			m.MarkWorkspaceIDs([]string{string(row.Workspace.MetadataID())})
		}
	}

	cmd := m.handleDelete()
	if cmd == nil {
		t.Fatal("expected bulk purge command")
	}
	msg, ok := cmd().(messages.ShowBulkPurgeWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowBulkPurgeWorkspaceDialog, got %T", cmd())
	}
	if len(msg.Items) != 2 {
		t.Fatalf("bulk items = %d, want 2", len(msg.Items))
	}
}

// TestMarkedShelvedItemsExcludesLiveRows proves the purge/restore
// collector structurally cannot pick up a marked live workspace.
func TestMarkedShelvedItemsExcludesLiveRows(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectWithTwoShelved()})
	// Mark everything: the live workspace row AND both shelved rows.
	for _, row := range m.rows {
		if row.Workspace != nil {
			m.MarkWorkspaceIDs([]string{string(row.Workspace.MetadataID())})
		}
	}
	items := m.markedShelvedItems()
	if len(items) != 2 {
		t.Fatalf("shelved items = %d, want 2 (live rows excluded)", len(items))
	}
	for _, it := range items {
		if !it.Workspace.Shelved {
			t.Fatalf("live workspace %q leaked into shelved items", it.Workspace.Name)
		}
	}
}

func TestDashboardShelvedRowRendersTagAndHelp(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	m.SetProjects([]data.Project{makeProjectWithShelved()})
	m.cursor = findRow(m, RowShelved)
	if m.cursor < 0 {
		t.Fatal("no shelved row")
	}

	rendered := ansi.Strip(m.View())
	if !strings.Contains(rendered, "shelved") {
		t.Fatal("shelved row should render a 'shelved' status tag")
	}
	if !strings.Contains(rendered, "shelved-ws") {
		t.Fatal("shelved row should render the workspace name")
	}
	help := ansi.Strip(strings.Join(m.helpLines(120), " "))
	if !strings.Contains(help, "restore") || !strings.Contains(help, "purge") {
		t.Fatalf("shelved row help = %q, want restore + purge", help)
	}
}
