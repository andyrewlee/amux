package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// workspaceCursor returns the index of the first workspace row, which is what
// the merge key acts on.
func workspaceCursor(t *testing.T, m *Model) int {
	t.Helper()
	for i, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil {
			return i
		}
	}
	t.Fatal("no workspace row in the dashboard")
	return -1
}

// TestMergeKeyRequestsMergeForWorkspaceRow asserts 'M' names the workspace
// under the cursor. Every precondition is the App's to check, so the dashboard's
// only job is to identify the target.
func TestMergeKeyRequestsMergeForWorkspaceRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = workspaceCursor(t, m)
	want := m.rows[m.cursor].Workspace

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'M', Text: "M"})
	if cmd == nil {
		t.Fatal("pressing 'M' on a workspace produced no command")
	}
	merge, ok := cmd().(messages.MergeWorkspace)
	if !ok {
		t.Fatalf("pressing 'M' emitted %T, want messages.MergeWorkspace", cmd())
	}
	if merge.Workspace != want {
		t.Fatal("merge request names the wrong workspace")
	}
}

// TestMergeKeyIgnoresProjectRow asserts a project row has nothing to merge —
// there is no branch — so the key is inert rather than guessing a workspace.
func TestMergeKeyIgnoresProjectRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	projectRow := -1
	for i, row := range m.rows {
		if row.Type == RowProject {
			projectRow = i
			break
		}
	}
	if projectRow == -1 {
		t.Skip("no project row rendered for this fixture")
	}
	m.cursor = projectRow

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'M', Text: "M"})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, isMerge := msg.(messages.MergeWorkspace); isMerge {
				t.Fatal("pressing 'M' on a project row requested a merge")
			}
		}
	}
}

// TestMergeKeyIsDistinctFromRefresh guards the binding: lowercase 'r' rescans
// and must never be mistaken for the merge action.
func TestMergeKeyIsDistinctFromRefresh(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = workspaceCursor(t, m)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("pressing 'r' produced no command")
	}
	if _, isMerge := cmd().(messages.MergeWorkspace); isMerge {
		t.Fatal("lowercase 'r' triggered a merge")
	}
}

// TestMergeAppearsInWorkspaceHelp asserts the action is discoverable on the
// rows it applies to, and absent on the rows it does not.
func TestMergeAppearsInWorkspaceHelp(t *testing.T) {
	m := New()
	m.SetShowKeymapHints(true)
	m.SetSize(120, 30)
	m.SetProjects([]data.Project{makeProject()})

	m.cursor = workspaceCursor(t, m)
	if help := ansi.Strip(strings.Join(m.helpLines(120), " ")); !strings.Contains(help, "merge") {
		t.Fatalf("workspace help does not advertise merge: %q", help)
	}

	for i, row := range m.rows {
		if row.Type == RowProject {
			m.cursor = i
			if help := ansi.Strip(strings.Join(m.helpLines(120), " ")); strings.Contains(help, "merge") {
				t.Fatalf("project help advertises merge, which does not apply: %q", help)
			}
			break
		}
	}
}
