package common

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// newTestFilePicker builds a picker rooted at a fresh temp dir so that the
// constructor's loadDirectory call succeeds without touching shared state.
func newTestFilePicker(t *testing.T) *FilePicker {
	t.Helper()
	return NewFilePicker("id", t.TempDir(), true)
}

func TestFilePickerSetShowKeymapHints(t *testing.T) {
	fp := newTestFilePicker(t)

	// Constructor defaults to true.
	if !fp.showKeymapHints {
		t.Fatalf("expected showKeymapHints to default to true")
	}

	tests := []struct {
		name string
		show bool
	}{
		{name: "disable hints", show: false},
		{name: "re-enable hints", show: true},
		{name: "toggle back off", show: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp.SetShowKeymapHints(tt.show)
			if fp.showKeymapHints != tt.show {
				t.Fatalf("SetShowKeymapHints(%v): got %v", tt.show, fp.showKeymapHints)
			}
		})
	}
}

func TestFilePickerSetStyles(t *testing.T) {
	fp := newTestFilePicker(t)

	custom := DefaultStyles()
	marker := lipgloss.NewStyle().SetString("custom-title")
	custom.Title = marker

	fp.SetStyles(custom)

	if got := fp.styles.Title.Value(); got != "custom-title" {
		t.Fatalf("SetStyles did not apply: got Title value %q, want %q", got, "custom-title")
	}

	// Applying a different Styles overwrites the previous one wholesale.
	replacement := DefaultStyles()
	replacement.Title = lipgloss.NewStyle().SetString("replacement")
	fp.SetStyles(replacement)
	if got := fp.styles.Title.Value(); got != "replacement" {
		t.Fatalf("SetStyles did not overwrite: got Title value %q, want %q", got, "replacement")
	}
}

func TestFilePickerSetTitle(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
	}{
		{name: "non-empty replaces default", input: "Pick a Repo", wantTitle: "Pick a Repo"},
		{name: "empty is ignored", input: "", wantTitle: "Select Directory"},
		{name: "whitespace is kept", input: " ", wantTitle: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := newTestFilePicker(t)
			// Sanity-check the constructor default before mutating.
			if fp.title != "Select Directory" {
				t.Fatalf("unexpected default title %q", fp.title)
			}
			fp.SetTitle(tt.input)
			if fp.title != tt.wantTitle {
				t.Fatalf("SetTitle(%q): got %q, want %q", tt.input, fp.title, tt.wantTitle)
			}
		})
	}
}

func TestFilePickerSetTitleEmptyKeepsPrevious(t *testing.T) {
	fp := newTestFilePicker(t)
	fp.SetTitle("Custom")
	fp.SetTitle("")
	if fp.title != "Custom" {
		t.Fatalf("empty SetTitle should keep previous title, got %q", fp.title)
	}
}

func TestFilePickerSetPrimaryActionLabel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLabel string
	}{
		{name: "non-empty replaces default", input: "Add", wantLabel: "Add"},
		{name: "empty is ignored", input: "", wantLabel: "Open"},
		{name: "whitespace is kept", input: " ", wantLabel: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := newTestFilePicker(t)
			if fp.primaryAction != "Open" {
				t.Fatalf("unexpected default primary action %q", fp.primaryAction)
			}
			fp.SetPrimaryActionLabel(tt.input)
			if fp.primaryAction != tt.wantLabel {
				t.Fatalf("SetPrimaryActionLabel(%q): got %q, want %q", tt.input, fp.primaryAction, tt.wantLabel)
			}
		})
	}
}

func TestFilePickerSetPrimaryActionLabelEmptyKeepsPrevious(t *testing.T) {
	fp := newTestFilePicker(t)
	fp.SetPrimaryActionLabel("Select")
	fp.SetPrimaryActionLabel("")
	if fp.primaryAction != "Select" {
		t.Fatalf("empty SetPrimaryActionLabel should keep previous label, got %q", fp.primaryAction)
	}
}

func TestFilePickerHideAndVisible(t *testing.T) {
	fp := newTestFilePicker(t)

	// A freshly constructed picker is hidden.
	if fp.Visible() {
		t.Fatalf("expected newly constructed picker to be hidden")
	}

	fp.Show()
	if !fp.Visible() {
		t.Fatalf("expected picker to be visible after Show")
	}

	fp.Hide()
	if fp.Visible() {
		t.Fatalf("expected picker to be hidden after Hide")
	}

	// Hide must be idempotent.
	fp.Hide()
	if fp.Visible() {
		t.Fatalf("expected picker to remain hidden after second Hide")
	}
}

func TestFilePickerMoveCursor(t *testing.T) {
	tests := []struct {
		name       string
		total      int
		startAt    int
		delta      int
		wantCursor int
	}{
		{name: "forward within bounds", total: 5, startAt: 1, delta: 1, wantCursor: 2},
		{name: "forward wraps to start", total: 3, startAt: 2, delta: 1, wantCursor: 0},
		{name: "backward within bounds", total: 5, startAt: 3, delta: -1, wantCursor: 2},
		{name: "backward wraps to end", total: 4, startAt: 0, delta: -1, wantCursor: 3},
		{name: "zero delta is a no-op", total: 5, startAt: 2, delta: 0, wantCursor: 2},
		{name: "positive delta only advances one", total: 5, startAt: 0, delta: 7, wantCursor: 1},
		{name: "negative delta only retreats one", total: 5, startAt: 4, delta: -7, wantCursor: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := newTestFilePicker(t)
			fp.filteredIdx = make([]int, tt.total)
			for i := range fp.filteredIdx {
				fp.filteredIdx[i] = i
			}
			fp.cursor = tt.startAt

			fp.moveCursor(tt.delta)

			if fp.cursor != tt.wantCursor {
				t.Fatalf("moveCursor(%d) from %d with total %d: got cursor %d, want %d",
					tt.delta, tt.startAt, tt.total, fp.cursor, tt.wantCursor)
			}
		})
	}
}

func TestFilePickerMoveCursorEmptyList(t *testing.T) {
	fp := newTestFilePicker(t)
	fp.filteredIdx = nil
	fp.cursor = 0
	fp.scrollOffset = 0

	// With nothing to display, moveCursor must not touch the cursor in either
	// direction and must not panic on the modulo/empty arithmetic.
	for _, delta := range []int{1, -1, 5} {
		fp.moveCursor(delta)
		if fp.cursor != 0 {
			t.Fatalf("moveCursor(%d) on empty list moved cursor to %d", delta, fp.cursor)
		}
	}
}

func TestFilePickerMoveCursorUpdatesScrollOffset(t *testing.T) {
	fp := newTestFilePicker(t)
	fp.maxVisible = 3
	fp.filteredIdx = make([]int, 10)
	for i := range fp.filteredIdx {
		fp.filteredIdx[i] = i
	}

	// Position the cursor at the bottom of the visible window, then advance:
	// ensureVisible (called inside moveCursor) must scroll the window down.
	fp.cursor = fp.maxVisible - 1 // 2
	fp.scrollOffset = 0
	fp.moveCursor(1)
	if fp.cursor != fp.maxVisible {
		t.Fatalf("expected cursor %d, got %d", fp.maxVisible, fp.cursor)
	}
	if fp.scrollOffset != fp.cursor-fp.maxVisible+1 {
		t.Fatalf("expected scrollOffset %d, got %d", fp.cursor-fp.maxVisible+1, fp.scrollOffset)
	}

	// Wrapping from the top back to the end should reset the offset to keep the
	// final row in view.
	fp.cursor = 0
	fp.scrollOffset = 0
	fp.moveCursor(-1)
	if fp.cursor != len(fp.filteredIdx)-1 {
		t.Fatalf("expected wrap to last index %d, got %d", len(fp.filteredIdx)-1, fp.cursor)
	}
	if fp.scrollOffset != fp.cursor-fp.maxVisible+1 {
		t.Fatalf("expected scrollOffset %d after wrap, got %d", fp.cursor-fp.maxVisible+1, fp.scrollOffset)
	}
}
