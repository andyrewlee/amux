package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// twoProjectFixture builds a model with two projects: "repo" holding
// workspaces "api" and "web", and "other" holding "db". Main-branch rows are
// skipped by rebuildRows, so the workspace rows are exactly api/web/db.
func twoProjectFixture() *Model {
	m := New()
	m.SetSize(80, 30)
	m.SetProjects([]data.Project{
		{
			Name: "repo",
			Path: "/repo",
			Workspaces: []data.Workspace{
				{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
				{Name: "api", Branch: "api-work", Repo: "/repo", Root: "/repo/.amux/ws/api"},
				{Name: "web", Branch: "web-ui", Repo: "/repo", Root: "/repo/.amux/ws/web"},
			},
		},
		{
			Name: "other",
			Path: "/other",
			Workspaces: []data.Workspace{
				{Name: "other", Branch: "main", Repo: "/other", Root: "/other"},
				{Name: "db", Branch: "db-migrate", Repo: "/other", Root: "/other/.amux/ws/db"},
			},
		},
	})
	return m
}

func workspaceRowNames(m *Model) []string {
	var names []string
	for _, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil {
			names = append(names, row.Workspace.Name)
		}
	}
	return names
}

func projectRowNames(m *Model) []string {
	var names []string
	for _, row := range m.rows {
		if row.Type == RowProject && row.Project != nil {
			names = append(names, row.Project.Name)
		}
	}
	return names
}

func typeKeys(m *Model, keys ...rune) {
	for _, r := range keys {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestDashboardSlashEntersFilterMode(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.filter.Active() {
		t.Fatal("/ should enter filter-input mode")
	}
	if !m.filter.Focused() {
		t.Fatal("/ should focus the filter input")
	}
}

func TestDashboardFilterNarrowsWorkspaceRows(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a', 'p', 'i')

	got := workspaceRowNames(m)
	if len(got) != 1 || got[0] != "api" {
		t.Fatalf("filtered workspace rows = %v, want [api]", got)
	}
	// The owning project survives because a child matched; "other" has no
	// match and its section collapses away.
	projs := projectRowNames(m)
	if len(projs) != 1 || projs[0] != "repo" {
		t.Fatalf("filtered project rows = %v, want [repo]", projs)
	}
}

func TestDashboardFilterProjectNameMatchKeepsSection(t *testing.T) {
	m := New()
	m.SetSize(80, 30)
	m.SetProjects([]data.Project{
		{
			Name: "other",
			Path: "/p/other",
			Workspaces: []data.Workspace{
				{Name: "other", Branch: "main", Repo: "/p/other", Root: "/p/other"},
				// Nothing in this row's name/branch/root contains the query —
				// only the project header itself can match.
				{Name: "db", Branch: "db-migrate", Repo: "/p/other", Root: "/data/db-files"},
			},
		},
	})
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'o', 't', 'h', 'e', 'r')

	// "other" matches on the project name itself: the header survives but
	// its non-matching workspace row is hidden.
	projs := projectRowNames(m)
	if len(projs) != 1 || projs[0] != "other" {
		t.Fatalf("filtered project rows = %v, want [other]", projs)
	}
	if got := workspaceRowNames(m); len(got) != 0 {
		t.Fatalf("filtered workspace rows = %v, want none", got)
	}
}

func TestDashboardFilterMatchesBranchAndRoot(t *testing.T) {
	for _, query := range []string{"db-migrate", ".amux/ws/db"} {
		m := twoProjectFixture()
		m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		typeKeys(m, []rune(query)...)
		got := workspaceRowNames(m)
		if len(got) != 1 || got[0] != "db" {
			t.Fatalf("query %q: workspace rows = %v, want [db]", query, got)
		}
	}
}

func TestDashboardFilterEnterKeepsQueryApplied(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a', 'p', 'i')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.filter.Active() {
		t.Fatal("enter should exit filter-input mode")
	}
	if got := m.filter.Query(); got != "api" {
		t.Fatalf("query = %q after enter, want api", got)
	}
	if got := workspaceRowNames(m); len(got) != 1 || got[0] != "api" {
		t.Fatalf("rows after enter = %v, want [api]", got)
	}
}

func TestDashboardFilterEscDismissesAppliedQuery(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a', 'p', 'i')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := workspaceRowNames(m); len(got) != 1 {
		t.Fatalf("precondition: want 1 filtered row, got %v", got)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filter.Query() != "" {
		t.Fatalf("esc should clear the applied query, got %q", m.filter.Query())
	}
	if got := workspaceRowNames(m); len(got) != 3 {
		t.Fatalf("esc should restore all rows, got %v", got)
	}
}

func TestDashboardFilterEscPrefersClearingMarks(t *testing.T) {
	m := twoProjectFixture()
	// Apply a filter, then mark a surviving row — esc must clear the mark
	// first, keeping the older esc contract.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a', 'p', 'i')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.MarkedCount() != 0 {
		t.Fatal("precondition: no marks")
	}
	// Land the cursor on the surviving workspace row — space only marks
	// workspace/shelved rows.
	for i, r := range m.rows {
		if r.Type == RowWorkspace {
			m.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.MarkedCount() != 1 {
		t.Fatalf("precondition: want 1 mark, got %d", m.MarkedCount())
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.MarkedCount() != 0 {
		t.Fatal("first esc should clear marks before the filter")
	}
	if m.filter.Query() == "" {
		t.Fatal("first esc should not clear the filter while marks exist")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filter.Query() != "" {
		t.Fatal("second esc should clear the filter")
	}
}

func TestDashboardFilterWhileActiveCapturesAllKeys(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	// 'g' would jump-to-top as a nav key; while filtering it types instead.
	typeKeys(m, 'g')
	if got := m.filter.Query(); got != "g" {
		t.Fatalf("Query() = %q, want %q — nav key must be captured", got, "g")
	}
	// Space would toggle a mark; while filtering it is part of the query.
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if got := m.filter.Query(); got != "g " {
		t.Fatalf("Query() = %q, want %q — space must be captured", got, "g ")
	}
	if m.MarkedCount() != 0 {
		t.Fatal("mark key should not toggle a mark while filtering")
	}
}

func TestDashboardFilterLineRendersActiveAndApplied(t *testing.T) {
	m := twoProjectFixture()
	if m.filterLine() != "" {
		t.Fatal("no filter line when no filter engaged")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if got := m.filterLine(); !strings.Contains(got, "/") {
		t.Fatalf("active filter line should render the / input, got %q", got)
	}
	typeKeys(m, 'a')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.filterLine(); !strings.Contains(got, "filter:") || !strings.Contains(got, "a") {
		t.Fatalf("applied filter line should render the query, got %q", got)
	}
}

func TestDashboardFilterBlurExitsInputKeepsQuery(t *testing.T) {
	m := twoProjectFixture()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a')
	m.Blur()
	if m.filter.Active() {
		t.Fatal("blur should exit filter-input mode")
	}
	if got := m.filter.Query(); got != "a" {
		t.Fatalf("blur should keep the applied query, got %q", got)
	}
}

func TestFilterDashboardRowsShelvedAndCreateSurvival(t *testing.T) {
	proj := &data.Project{Name: "repo", Path: "/repo"}
	shelved := data.Workspace{Name: "old-api", Branch: "old", Root: "/shelved/old-api"}
	rows := []Row{
		{Type: RowHome},
		{Type: RowSpacer},
		{Type: RowProject, Project: proj},
		{Type: RowWorkspace, Project: proj, Workspace: &data.Workspace{Name: "web", Root: "/w/web"}},
		{Type: RowShelved, Project: proj, Workspace: &shelved},
		{Type: RowCreate, Project: proj},
		{Type: RowSpacer},
	}
	got := filterDashboardRows(rows, "old-api")
	var types []RowType
	for _, r := range got {
		types = append(types, r.Type)
	}
	// Home, spacer, project (child match), the matching shelved row, the
	// surviving project's create row, trailing spacer.
	want := []RowType{RowHome, RowSpacer, RowProject, RowShelved, RowCreate, RowSpacer}
	if len(types) != len(want) {
		t.Fatalf("filtered types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("filtered types = %v, want %v", types, want)
		}
	}
}

// A routed PasteMsg lands in the live filter like typed keys — the row set
// narrows on the pasted query. (The app router only delivers paste while
// FilterActive; this covers the in-model half of that contract.)
func TestDashboardFilterPasteNarrowsRows(t *testing.T) {
	m := twoProjectFixture()
	m.Focus()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.PasteMsg{Content: "api"})

	got := workspaceRowNames(m)
	if len(got) != 1 || got[0] != "api" {
		t.Fatalf("paste-filtered workspace rows = %v, want [api]", got)
	}
	if !m.filter.Active() {
		t.Fatal("paste exited filter-input mode — only Enter accepts")
	}
}

// A paste with no live filter is ignored — the FilterInput refuses it.
func TestDashboardPasteWithoutFilterIgnored(t *testing.T) {
	m := twoProjectFixture()
	m.Focus()
	m.Update(tea.PasteMsg{Content: "api"})
	if got := workspaceRowNames(m); len(got) != 3 {
		t.Fatalf("paste without a filter narrowed rows to %v", got)
	}
}

func TestDashboardFilterCursorClampsAfterNarrowing(t *testing.T) {
	m := twoProjectFixture()
	// Park the cursor on the last row, then narrow to one workspace — the
	// cursor must land inside the filtered list.
	if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
		m.cursor = idx
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeKeys(m, 'a', 'p', 'i')
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		t.Fatalf("cursor %d out of bounds for %d rows", m.cursor, len(m.rows))
	}
	if !isSelectable(m.rows[m.cursor].Type) {
		t.Fatalf("cursor landed on non-selectable row type %d", m.rows[m.cursor].Type)
	}
}
