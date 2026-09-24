package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// makeProjectMulti returns a project with two non-main workspace rows so
// mark/bulk tests have more than one markable row.
func makeProjectMulti() data.Project {
	return data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
			{Name: "alpha", Branch: "alpha", Repo: "/repo", Root: "/repo/.amux/workspaces/alpha"},
			{Name: "beta", Branch: "beta", Repo: "/repo", Root: "/repo/.amux/workspaces/beta"},
		},
	}
}

// workspaceRowIndexByName finds the row index of a workspace by name.
func workspaceRowIndexByName(m *Model, name string) int {
	for i, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Name == name {
			return i
		}
	}
	return -1
}

func pressKey(m *Model, code rune, text string) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	return cmd
}

func TestDashboardSpaceTogglesMark(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	m.cursor = workspaceRowIndexByName(m, "alpha")
	if m.cursor < 0 {
		t.Fatal("no alpha row")
	}

	pressKey(m, ' ', " ")
	if m.MarkedCount() != 1 {
		t.Fatalf("after mark: count = %d, want 1", m.MarkedCount())
	}
	pressKey(m, ' ', " ")
	if m.MarkedCount() != 0 {
		t.Fatalf("after unmark: count = %d, want 0", m.MarkedCount())
	}
}

func TestDashboardMarkSurvivesRebuild(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	m.cursor = workspaceRowIndexByName(m, "alpha")
	pressKey(m, ' ', " ")

	// A projects reload rebuilds rows wholesale; the mark, keyed by
	// MetadataID, must still match the same workspace's new row.
	m.SetProjects([]data.Project{makeProjectMulti()})
	if m.MarkedCount() != 1 {
		t.Fatalf("after rebuild: count = %d, want 1", m.MarkedCount())
	}
	row := m.rows[workspaceRowIndexByName(m, "alpha")]
	if !m.isMarked(row) {
		t.Fatal("alpha row lost its mark across rebuild")
	}
}

func TestDashboardEscClearsMarks(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	for _, name := range []string{"alpha", "beta"} {
		m.cursor = workspaceRowIndexByName(m, name)
		pressKey(m, ' ', " ")
	}
	if m.MarkedCount() != 2 {
		t.Fatalf("setup: count = %d, want 2", m.MarkedCount())
	}
	pressKey(m, tea.KeyEscape, "")
	if m.MarkedCount() != 0 {
		t.Fatalf("after esc: count = %d, want 0", m.MarkedCount())
	}
}

func TestDashboardMarkedRowRendersGlyph(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	m.cursor = workspaceRowIndexByName(m, "alpha")
	pressKey(m, ' ', " ")

	rendered := ansi.Strip(m.View())
	if !strings.Contains(rendered, "●alpha") && !strings.Contains(rendered, "● alpha") {
		t.Fatalf("marked row should render the ● glyph; view:\n%s", rendered)
	}
	help := ansi.Strip(strings.Join(m.helpLines(120), " "))
	if !strings.Contains(help, "clear 1 marks") {
		t.Fatalf("help = %q, want 'clear 1 marks'", help)
	}
}

func TestDashboardShelveKeyEmitsBulkDialogWhenMarked(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	for _, name := range []string{"alpha", "beta"} {
		m.cursor = workspaceRowIndexByName(m, name)
		pressKey(m, ' ', " ")
	}
	// Cursor on beta; marks cover alpha+beta — S must emit the bulk dialog
	// with exactly the marked set in display order, not the single row.
	m.cursor = workspaceRowIndexByName(m, "beta")
	cmd := pressKey(m, 'S', "S")
	if cmd == nil {
		t.Fatal("expected command for 'S' with marks")
	}
	msg := cmd()
	bulk, ok := msg.(messages.ShowBulkShelveWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowBulkShelveWorkspaceDialog, got %T", msg)
	}
	if len(bulk.Items) != 2 {
		t.Fatalf("bulk items = %d, want 2", len(bulk.Items))
	}
	if bulk.Items[0].Workspace.Name != "alpha" || bulk.Items[1].Workspace.Name != "beta" {
		t.Fatalf("items order = %q,%q, want alpha,beta (display order)",
			bulk.Items[0].Workspace.Name, bulk.Items[1].Workspace.Name)
	}
	for _, it := range bulk.Items {
		if it.Project == nil || it.Workspace == nil {
			t.Fatal("bulk item missing project/workspace")
		}
	}
}

func TestDashboardShelveKeySingleWhenNoMarks(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProjectMulti()})
	m.Focus()
	m.cursor = workspaceRowIndexByName(m, "alpha")
	cmd := pressKey(m, 'S', "S")
	if cmd == nil {
		t.Fatal("expected command for 'S' without marks")
	}
	if _, ok := cmd().(messages.ShowShelveWorkspaceDialog); !ok {
		t.Fatalf("expected ShowShelveWorkspaceDialog, got %T", cmd())
	}
}

func TestDashboardMarkedShelvedRowExcludedFromBulk(t *testing.T) {
	m := New()
	p := makeProjectMulti()
	p.ShelvedWorkspaces = []data.Workspace{
		{Name: "shelved-ws", Branch: "shelved", Repo: "/repo", Root: "/repo/.amux/workspaces/shelved", Archived: true, Shelved: true},
	}
	m.SetProjects([]data.Project{p})
	m.Focus()

	// Mark the shelved row AND one live row; bulk items must contain only
	// the live row — shelving a shelved workspace is meaningless.
	m.cursor = findRow(m, RowShelved)
	pressKey(m, ' ', " ")
	m.cursor = workspaceRowIndexByName(m, "alpha")
	pressKey(m, ' ', " ")

	cmd := pressKey(m, 'S', "S")
	if cmd == nil {
		t.Fatal("expected command for 'S' with mixed marks")
	}
	bulk, ok := cmd().(messages.ShowBulkShelveWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowBulkShelveWorkspaceDialog, got %T", cmd())
	}
	if len(bulk.Items) != 1 || bulk.Items[0].Workspace.Name != "alpha" {
		t.Fatalf("bulk items = %+v, want exactly [alpha]", bulk.Items)
	}
	// The shelved row's mark stays — a future bulk restore/purge uses it.
	if m.MarkedCount() != 2 {
		t.Fatalf("shelved mark should persist: count = %d, want 2", m.MarkedCount())
	}
}
