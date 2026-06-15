package dashboard

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestDashboardHelpItem verifies that helpItem renders a key/description pair
// using the model's styles, matching common.RenderHelpItem exactly.
func TestDashboardHelpItem(t *testing.T) {
	tests := []struct {
		name string
		key  string
		desc string
	}{
		{name: "typical key and desc", key: "enter", desc: "open"},
		{name: "single char key", key: "G", desc: "bottom"},
		{name: "chord key", key: "C-Space", desc: "Commands"},
		{name: "empty key", key: "", desc: "open"},
		{name: "empty desc", key: "enter", desc: ""},
		{name: "both empty", key: "", desc: ""},
		{name: "unicode key", key: "↑", desc: "up"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()

			got := m.helpItem(tt.key, tt.desc)
			want := common.RenderHelpItem(m.styles, tt.key, tt.desc)
			if got != want {
				t.Fatalf("helpItem(%q, %q) = %q, want %q", tt.key, tt.desc, got, want)
			}

			// The rendered item must contain the key and the colon-prefixed
			// description as plain text, regardless of styling escape codes.
			if tt.key != "" && !strings.Contains(got, tt.key) {
				t.Fatalf("helpItem output %q does not contain key %q", got, tt.key)
			}
			if !strings.Contains(got, ":"+tt.desc) {
				t.Fatalf("helpItem output %q does not contain %q", got, ":"+tt.desc)
			}
		})
	}
}

// TestDashboardHelpItemUsesModelStyles confirms helpItem reflects the model's
// current styles rather than package defaults.
func TestDashboardHelpItemUsesModelStyles(t *testing.T) {
	m := New()
	custom := common.DefaultStyles()
	custom.HelpKey = custom.HelpKey.Bold(true)
	m.SetStyles(custom)

	got := m.helpItem("r", "rescan")
	want := common.RenderHelpItem(custom, "r", "rescan")
	if got != want {
		t.Fatalf("helpItem did not use model styles: got %q, want %q", got, want)
	}
}

// helpJoin concatenates wrapped help lines into a single string so tests can
// assert on the presence of logical items independent of line wrapping.
func helpJoin(lines []string) string {
	return strings.Join(lines, "\n")
}

// helpContains reports whether the wrapped help output contains an item with
// the given description (the ":desc" suffix is unique per item here).
func helpContains(lines []string, desc string) bool {
	return strings.Contains(helpJoin(lines), ":"+desc)
}

// TestDashboardHelpLinesBaseItems verifies the always-present navigation items
// are emitted regardless of cursor position.
func TestDashboardHelpLinesBaseItems(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Wide width keeps everything on as few lines as possible, but the items
	// themselves must always be present.
	lines := m.helpLines(500)

	for _, desc := range []string{"up", "down", "open", "rescan", "top", "bottom", "Commands", "Settings", "quit"} {
		if !helpContains(lines, desc) {
			t.Fatalf("expected help lines to contain item with desc %q, got %v", desc, lines)
		}
	}
}

// TestDashboardHelpLinesCursorContextItem verifies the cursor-dependent item
// switches between "delete" (workspace), "remove" (project), and absent.
func TestDashboardHelpLinesCursorContextItem(t *testing.T) {
	// Rows produced by makeProject():
	//   0 RowHome, 1 RowSpacer, 2 RowProject, 3 RowWorkspace, 4 RowCreate, 5 RowSpacer
	tests := []struct {
		name        string
		cursor      int
		wantDesc    string // desc that must be present ("" means neither delete nor remove)
		absentDesc  string // desc that must be absent
		wantRowType RowType
	}{
		{name: "project row offers remove", cursor: 2, wantDesc: "remove", absentDesc: "delete", wantRowType: RowProject},
		{name: "workspace row offers delete", cursor: 3, wantDesc: "delete", absentDesc: "remove", wantRowType: RowWorkspace},
		{name: "home row offers neither", cursor: 0, wantDesc: "", wantRowType: RowHome},
		{name: "spacer row offers neither", cursor: 1, wantDesc: "", wantRowType: RowSpacer},
		{name: "create row offers neither", cursor: 4, wantDesc: "", wantRowType: RowCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetProjects([]data.Project{makeProject()})

			if tt.cursor >= len(m.rows) {
				t.Fatalf("test setup invalid: cursor %d out of range for %d rows", tt.cursor, len(m.rows))
			}
			if got := m.rows[tt.cursor].Type; got != tt.wantRowType {
				t.Fatalf("row %d type = %v, want %v (fixture layout changed)", tt.cursor, got, tt.wantRowType)
			}

			m.cursor = tt.cursor
			lines := m.helpLines(500)

			if tt.wantDesc != "" {
				if !helpContains(lines, tt.wantDesc) {
					t.Fatalf("cursor %d: expected context item %q, got %v", tt.cursor, tt.wantDesc, lines)
				}
				if tt.absentDesc != "" && helpContains(lines, tt.absentDesc) {
					t.Fatalf("cursor %d: did not expect item %q, got %v", tt.cursor, tt.absentDesc, lines)
				}
			} else {
				if helpContains(lines, "delete") {
					t.Fatalf("cursor %d (%v): unexpected 'delete' context item, got %v", tt.cursor, tt.wantRowType, lines)
				}
				if helpContains(lines, "remove") {
					t.Fatalf("cursor %d (%v): unexpected 'remove' context item, got %v", tt.cursor, tt.wantRowType, lines)
				}
			}

			// Base items are always present regardless of cursor context.
			for _, desc := range []string{"up", "down", "open", "rescan", "quit"} {
				if !helpContains(lines, desc) {
					t.Fatalf("cursor %d: expected base item %q, got %v", tt.cursor, desc, lines)
				}
			}
		})
	}
}

// TestDashboardHelpLinesOutOfRangeCursor verifies that an out-of-range cursor
// (no rows, or index past the end / negative) does not panic and emits only the
// base items with no context-specific entry.
func TestDashboardHelpLinesOutOfRangeCursor(t *testing.T) {
	tests := []struct {
		name    string
		project bool
		cursor  int
	}{
		{name: "no rows, cursor zero", project: false, cursor: 0},
		{name: "negative cursor", project: true, cursor: -1},
		{name: "cursor past end", project: true, cursor: 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			if tt.project {
				m.SetProjects([]data.Project{makeProject()})
			}
			m.cursor = tt.cursor

			lines := m.helpLines(500)

			if helpContains(lines, "delete") || helpContains(lines, "remove") {
				t.Fatalf("out-of-range cursor %d must not emit context item, got %v", tt.cursor, lines)
			}
			// Base navigation items must still be present.
			if !helpContains(lines, "up") || !helpContains(lines, "quit") {
				t.Fatalf("out-of-range cursor %d dropped base items, got %v", tt.cursor, lines)
			}
		})
	}
}

// TestDashboardHelpLinesWrapping verifies width drives the number of wrapped
// lines: a narrow width forces more lines than a very wide width.
func TestDashboardHelpLinesWrapping(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 3 // workspace row, so the full set of items is present

	wide := m.helpLines(1000)
	narrow := m.helpLines(10)

	if len(wide) < 1 {
		t.Fatalf("expected at least one wide help line, got %d", len(wide))
	}
	if len(narrow) <= len(wide) {
		t.Fatalf("expected narrow width to wrap into more lines than wide: narrow=%d wide=%d", len(narrow), len(wide))
	}

	// Wrapping must preserve every item: the joined content is identical
	// regardless of how many lines it spans.
	for _, desc := range []string{"up", "down", "open", "delete", "rescan", "top", "bottom", "Commands", "Settings", "quit"} {
		if !helpContains(wide, desc) {
			t.Fatalf("wide wrap dropped item %q, got %v", desc, wide)
		}
		if !helpContains(narrow, desc) {
			t.Fatalf("narrow wrap dropped item %q, got %v", desc, narrow)
		}
	}
}

// TestDashboardHelpLinesNonPositiveWidth verifies the width<=0 boundary: all
// items collapse onto a single joined line (per common.WrapHelpItems).
func TestDashboardHelpLinesNonPositiveWidth(t *testing.T) {
	for _, width := range []int{0, -1, -100} {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.cursor = 3

		lines := m.helpLines(width)
		if len(lines) != 1 {
			t.Fatalf("width %d: expected single collapsed line, got %d lines: %v", width, len(lines), lines)
		}
		if !helpContains(lines, "up") || !helpContains(lines, "quit") {
			t.Fatalf("width %d: collapsed line missing items, got %v", width, lines)
		}
	}
}

// TestDashboardHelpLineCountMatchesHelpLines verifies helpLineCount is a faithful
// proxy for len(helpLines) when hints are enabled, and zero when disabled.
func TestDashboardHelpLineCountMatchesHelpLines(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.SetSize(80, 40)
	m.cursor = 3

	m.SetShowKeymapHints(false)
	if got := m.helpLineCount(); got != 0 {
		t.Fatalf("expected helpLineCount 0 with hints disabled, got %d", got)
	}

	m.SetShowKeymapHints(true)
	want := len(m.helpLines(m.width - 3))
	if got := m.helpLineCount(); got != want {
		t.Fatalf("helpLineCount = %d, want len(helpLines(width-3)) = %d", got, want)
	}
}

// TestDashboardHelpLineCountClampsContentWidth verifies the contentWidth floor
// of 1 is applied for tiny widths so helpLineCount stays positive and finite.
func TestDashboardHelpLineCountClampsContentWidth(t *testing.T) {
	for _, width := range []int{0, 1, 2, 3, 4} {
		m := New()
		m.SetProjects([]data.Project{makeProject()})
		m.SetSize(width, 40)
		m.cursor = 3
		m.SetShowKeymapHints(true)

		got := m.helpLineCount()
		if got <= 0 {
			t.Fatalf("width %d: expected positive help line count, got %d", width, got)
		}
	}
}
