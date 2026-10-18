package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// Key→handler coverage for the dashboard nav/toolbar bindings that
// previously only had direct method calls (moveCursor/findSelectableRow)
// or no coverage at all. An unbound or remapped key ships green without
// these. Covered elsewhere: j/k rows, D/d, M, S, r, space, esc, `/`,
// toolbar enter (model_actions_test.go, dashboard_filter_test.go,
// dashboard_marks_test.go, toolbar_click_test.go, merge_key_test.go,
// dashboard_shelve_test.go, dashboard_select_workspace_test.go).

func keyedDashboard(t *testing.T) *Model {
	t.Helper()
	m := twoProjectFixture()
	m.focused = true
	_ = m.View()
	return m
}

func firstWorkspaceRow(t *testing.T, m *Model) int {
	t.Helper()
	for i, row := range m.rows {
		if row.Type == RowWorkspace {
			return i
		}
	}
	t.Fatal("fixture has no workspace row")
	return -1
}

func TestDashboardKey_PgDownPgUpHalfPage(t *testing.T) {
	m := keyedDashboard(t)
	m.cursor = 0

	half := m.visibleHeight() / 2
	if half < 1 {
		half = 1
	}

	// Update mutates and returns the same model — snapshot cursor between
	// presses instead of comparing receivers.
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	afterPgDown := m2.cursor
	want := half
	if last := len(m2.rows) - 1; want > last {
		want = last
	}
	if afterPgDown == 0 || afterPgDown > want+1 {
		t.Fatalf("pgdown moved cursor to %d, want roughly +%d rows", afterPgDown, half)
	}

	m3, _ := m2.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	afterCtrlU := m3.cursor
	if afterCtrlU >= afterPgDown {
		t.Fatalf("ctrl+u did not move cursor up: %d -> %d", afterPgDown, afterCtrlU)
	}

	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	afterCtrlD := m4.cursor
	if afterCtrlD <= afterCtrlU {
		t.Fatalf("ctrl+d did not move cursor down: %d -> %d", afterCtrlU, afterCtrlD)
	}

	m5, _ := m4.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m5.cursor >= afterCtrlD {
		t.Fatalf("pgup did not move cursor up: %d -> %d", afterCtrlD, m5.cursor)
	}
}

func TestDashboardKey_GAndg_JumpEnds(t *testing.T) {
	m := keyedDashboard(t)
	m.cursor = 0

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	last := m2.findSelectableRow(len(m2.rows)-1, -1)
	if last == -1 {
		t.Fatal("fixture has no selectable row")
	}
	if m2.cursor != last {
		t.Fatalf("G moved cursor to %d, want last selectable %d", m2.cursor, last)
	}
	// Activation rides along only for rows that activate — the trailing
	// "+ New" create row is selectable but emits no cmd.
	if isSelectable(m2.rows[last].Type) && m2.rows[last].Type != RowCreate && cmd == nil {
		t.Fatalf("G onto a %v row should emit the activation cmd", m2.rows[last].Type)
	}

	m3, _ := m2.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	first := m3.findSelectableRow(0, 1)
	if m3.cursor != first {
		t.Fatalf("g moved cursor to %d, want first selectable %d", m3.cursor, first)
	}
}

func TestDashboardKey_R_EmitsRenameDialog(t *testing.T) {
	m := keyedDashboard(t)
	m.cursor = firstWorkspaceRow(t, m)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from R on a workspace row")
	}
	if _, ok := cmd().(messages.ShowRenameWorkspaceDialog); !ok {
		t.Fatalf("expected messages.ShowRenameWorkspaceDialog, got %T", cmd())
	}
}

func TestDashboardKey_EnterActivatesWorkspaceRow(t *testing.T) {
	m := keyedDashboard(t)
	m.cursor = firstWorkspaceRow(t, m)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter on a workspace row")
	}
	if _, ok := cmd().(messages.WorkspaceActivated); !ok {
		t.Fatalf("expected messages.WorkspaceActivated, got %T", cmd())
	}
}

func TestDashboardKey_JOnLastRowFocusesToolbar(t *testing.T) {
	m := keyedDashboard(t)
	m.cursor = m.findSelectableRow(len(m.rows)-1, -1)
	if m.cursor == -1 {
		t.Fatal("fixture has no selectable row")
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !m2.toolbarFocused {
		t.Fatal("j on the last selectable row should move focus to the toolbar")
	}
	if m2.toolbarIndex != 0 {
		t.Fatalf("toolbar entry should land on index 0, got %d", m2.toolbarIndex)
	}
}

func TestDashboardKey_ToolbarCycleAndExit(t *testing.T) {
	m := keyedDashboard(t)
	m.toolbarFocused = true
	m.toolbarIndex = 0

	// l / right cycles forward, h / left cycles back.
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m2.toolbarIndex != 1 {
		t.Fatalf("l should move toolbar index to 1, got %d", m2.toolbarIndex)
	}
	m3, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m3.toolbarIndex != 0 {
		t.Fatalf("right should wrap toolbar index to 0, got %d", m3.toolbarIndex)
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m4.toolbarIndex != 1 {
		t.Fatalf("h should wrap toolbar index back to 1, got %d", m4.toolbarIndex)
	}

	// up/down/j/k exit the toolbar onto the last selectable row.
	for _, keyMsg := range []tea.KeyPressMsg{
		{Code: tea.KeyUp},
		{Code: tea.KeyDown},
		{Code: 'k', Text: "k"},
		{Code: 'j', Text: "j"},
	} {
		mm := keyedDashboard(t)
		mm.toolbarFocused = true
		mm.toolbarIndex = 0
		out, _ := mm.Update(keyMsg)
		if out.toolbarFocused {
			t.Fatalf("key %v should exit toolbar focus", keyMsg.Code)
		}
		if last := out.findSelectableRow(len(out.rows)-1, -1); last != -1 && out.cursor != last {
			t.Fatalf("key %v should land cursor on last selectable row %d, got %d", keyMsg.Code, last, out.cursor)
		}
	}
}
