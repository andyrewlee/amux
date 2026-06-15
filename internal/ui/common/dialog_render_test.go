package common

import (
	"strings"
	"testing"
)

// TestDialogViewHiddenReturnsEmpty verifies View renders nothing while the
// dialog is hidden, regardless of dialog type.
func TestDialogViewHiddenReturnsEmpty(t *testing.T) {
	tests := []struct {
		name string
		dlg  *Dialog
	}{
		{name: "input", dlg: NewInputDialog("id", "Title", "Placeholder")},
		{name: "confirm", dlg: NewConfirmDialog("id", "Title", "Message")},
		{name: "select", dlg: NewSelectDialog("id", "Title", "Msg", []string{"A", "B"})},
		{name: "agent-picker", dlg: NewAgentPicker([]string{"claude", "codex"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A fresh dialog is hidden until Show is called.
			if tt.dlg.Visible() {
				t.Fatalf("expected fresh dialog to be hidden")
			}
			if got := tt.dlg.View(); got != "" {
				t.Fatalf("expected empty view while hidden, got %q", got)
			}

			// Showing then hiding must again yield an empty view.
			tt.dlg.Show()
			tt.dlg.Hide()
			if got := tt.dlg.View(); got != "" {
				t.Fatalf("expected empty view after Hide, got %q", got)
			}
		})
	}
}

// TestDialogViewVisibleRendersContent verifies View produces a bordered,
// non-empty rendering that contains the dialog's title and key content once
// the dialog is shown.
func TestDialogViewVisibleRendersContent(t *testing.T) {
	tests := []struct {
		name string
		dlg  func() *Dialog
		// substrings that must appear in the rendered view.
		wantContains []string
	}{
		{
			name: "input dialog shows title and buttons",
			dlg: func() *Dialog {
				return NewInputDialog("create", "Create Workspace", "name...")
			},
			wantContains: []string{"Create Workspace", "OK", "Cancel"},
		},
		{
			name: "confirm dialog shows title, message and options",
			dlg: func() *Dialog {
				return NewConfirmDialog("quit", "Quit?", "Are you sure?")
			},
			wantContains: []string{"Quit?", "Are you sure?", "Yes", "No"},
		},
		{
			name: "select dialog shows title, message and options",
			dlg: func() *Dialog {
				return NewSelectDialog("act", "Action", "Pick one:", []string{"Edit", "Delete"})
			},
			wantContains: []string{"Action", "Pick one:", "Edit", "Delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.dlg()
			d.SetSize(80, 24)
			d.Show()

			view := d.View()
			if view == "" {
				t.Fatalf("expected non-empty view for visible dialog")
			}
			// A rendered dialog has a rounded border, so the top-left corner
			// glyph must be present.
			if !strings.Contains(view, "╭") {
				t.Fatalf("expected rendered border in view, got:\n%s", view)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(view, want) {
					t.Fatalf("expected view to contain %q, got:\n%s", want, view)
				}
			}
		})
	}
}

// TestDialogViewMatchesRenderLines verifies View is exactly the styled join of
// renderLines, i.e. it delegates rendering and applies the dialog style.
func TestDialogViewMatchesRenderLines(t *testing.T) {
	d := NewConfirmDialog("quit", "Quit?", "Are you sure?")
	d.SetSize(80, 24)
	d.Show()

	lines := d.renderLines()
	want := d.dialogStyle().Render(strings.Join(lines, "\n"))

	if got := d.View(); got != want {
		t.Fatalf("View() did not match styled renderLines join\n got: %q\nwant: %q", got, want)
	}
}

// TestDialogViewHonorsKeymapHints verifies the keymap help line only appears in
// the rendered view when hints are enabled.
func TestDialogViewHonorsKeymapHints(t *testing.T) {
	makeDialog := func() *Dialog {
		d := NewConfirmDialog("quit", "Quit?", "Are you sure?")
		d.SetSize(80, 24)
		return d
	}

	// Hints disabled (default): the help text must not appear.
	off := makeDialog()
	off.Show()
	if strings.Contains(off.View(), "enter: confirm") {
		t.Fatalf("did not expect keymap hints when disabled, got:\n%s", off.View())
	}

	// Hints enabled: the confirm help text must appear.
	on := makeDialog()
	on.SetShowKeymapHints(true)
	on.Show()
	view := on.View()
	if !strings.Contains(view, on.helpText()) {
		t.Fatalf("expected keymap hints %q in view, got:\n%s", on.helpText(), view)
	}
}

// TestDialogViewSelectFilterEnabled verifies a filter-enabled select dialog
// (the agent picker) renders its filter prompt and options, and shows the
// "No matches" placeholder when the filter excludes everything.
func TestDialogViewSelectFilterEnabled(t *testing.T) {
	d := NewAgentPicker([]string{"claude", "codex"})
	d.SetSize(80, 24)
	d.Show()

	view := d.View()
	if view == "" {
		t.Fatalf("expected non-empty view for visible agent picker")
	}
	for _, want := range []string{"New Agent", "Select agent type:", "claude", "codex"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected agent picker view to contain %q, got:\n%s", want, view)
		}
	}

	// Filtering to a non-matching pattern collapses all options and renders
	// the "No matches" placeholder.
	d.filterInput.SetValue("zzzzz")
	d.applyFilter()
	noMatch := d.View()
	if !strings.Contains(noMatch, "No matches") {
		t.Fatalf("expected 'No matches' placeholder, got:\n%s", noMatch)
	}
	if strings.Contains(noMatch, "[claude]") || strings.Contains(noMatch, "[codex]") {
		t.Fatalf("expected no option rows when filter matches nothing, got:\n%s", noMatch)
	}
}

// TestDialogHelpText exercises every branch of helpText, including the
// filter-enabled vs. plain select cases and the default fall-through for an
// unset dialog type.
func TestDialogHelpText(t *testing.T) {
	tests := []struct {
		name  string
		setup func() *Dialog
		want  string
	}{
		{
			name: "input dialog",
			setup: func() *Dialog {
				return NewInputDialog("id", "Title", "Placeholder")
			},
			want: "enter: confirm • esc: cancel • click OK/Cancel",
		},
		{
			name: "confirm dialog",
			setup: func() *Dialog {
				return NewConfirmDialog("id", "Title", "Message")
			},
			want: "h/l or tab: choose • enter: confirm • esc: cancel",
		},
		{
			name: "select dialog without filter",
			setup: func() *Dialog {
				return NewSelectDialog("id", "Title", "Msg", []string{"A", "B"})
			},
			want: "↑/↓ or tab: move • enter: select • esc: cancel",
		},
		{
			name: "select dialog with filter",
			setup: func() *Dialog {
				return NewAgentPicker([]string{"claude"})
			},
			want: "type to filter • ↑/↓ or tab: move • enter: select • esc: cancel",
		},
		{
			name: "default for unset dialog type",
			setup: func() *Dialog {
				// DialogNone (the zero value) hits the default branch.
				return &Dialog{dtype: DialogNone}
			},
			want: "enter: confirm • esc: cancel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.setup()
			if got := d.helpText(); got != tt.want {
				t.Fatalf("helpText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDialogHelpTextSelectFilterToggle verifies the select dialog's help text
// switches based solely on whether the fuzzy filter is enabled.
func TestDialogHelpTextSelectFilterToggle(t *testing.T) {
	d := NewSelectDialog("id", "Title", "Msg", []string{"A", "B"})

	plain := d.helpText()
	if strings.Contains(plain, "type to filter") {
		t.Fatalf("plain select should not mention filtering, got %q", plain)
	}

	// Enabling the filter must switch to the filter-aware help string.
	d.filterEnabled = true
	filtered := d.helpText()
	if !strings.HasPrefix(filtered, "type to filter") {
		t.Fatalf("filter-enabled select should mention filtering, got %q", filtered)
	}
	if filtered == plain {
		t.Fatalf("expected help text to change when filter is enabled")
	}
}
