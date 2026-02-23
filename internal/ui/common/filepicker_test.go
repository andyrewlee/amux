package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestFilePickerCursorHiddenWhenNotVisible(t *testing.T) {
	fp := NewFilePicker("id", t.TempDir(), true)
	if c := fp.Cursor(); c != nil {
		t.Fatalf("expected nil cursor when file picker is hidden, got %+v", c)
	}
}

func TestFilePickerCursorPosition(t *testing.T) {
	fp := NewFilePicker("id", t.TempDir(), true)
	fp.Show()
	fp.input.SetValue("abc")
	fp.input.SetCursor(3)

	inputCursor := fp.input.Cursor()
	if inputCursor == nil {
		t.Fatalf("expected input cursor, got nil")
	}

	c := fp.Cursor()
	if c == nil {
		t.Fatalf("expected file picker cursor, got nil")
	}

	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + fp.inputOffset() + 2

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}

func TestFilePickerBackspaceThroughSeparatorNavigatesToParent(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", nested, true)
	fp.Show()

	// Simulate user backspacing past a separator: input is now the parent path
	parent := filepath.Dir(nested)
	fp.input.SetValue(parent)
	fp.handlePathInput(parent)

	if fp.currentPath != parent {
		t.Fatalf("expected current path %q, got %q", parent, fp.currentPath)
	}
}

func TestFilePickerForwardNavigationUpdatesEntries(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "src")
	if err := os.MkdirAll(filepath.Join(sub, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "other"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	// Simulate typing "src/" manually (with trailing slash)
	newInput := fp.inputBasePath() + "src/"
	fp.input.SetValue(newInput)
	fp.handlePathInput(newInput)

	// currentPath should have navigated into the src subdirectory
	if fp.currentPath != sub {
		t.Fatalf("expected currentPath %q, got %q", sub, fp.currentPath)
	}

	// Now type "f" to filter within src/
	newInput2 := fp.inputBasePath() + "f"
	fp.input.SetValue(newInput2)
	fp.handlePathInput(newInput2)

	if fp.filteredIdx == nil {
		t.Fatalf("expected suggestions after typing within navigated dir")
	}
	if len(fp.filteredIdx) != 1 {
		t.Fatalf("expected 1 suggestion (foo), got %d", len(fp.filteredIdx))
	}
	if fp.entries[fp.filteredIdx[0]].Name() != "foo" {
		t.Fatalf("expected suggestion 'foo', got %q", fp.entries[fp.filteredIdx[0]].Name())
	}
}

func TestFilePickerPathInputOutsideCurrentDoesNotNavigate(t *testing.T) {
	tmp := t.TempDir()
	other := t.TempDir()

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	// Typing a completely different path should not navigate
	fp.input.SetValue(other)
	fp.handlePathInput(other)

	if fp.currentPath != tmp {
		t.Fatalf("expected current path to remain %q, got %q", tmp, fp.currentPath)
	}
}

func TestFilePickerSuggestionsHiddenByDefault(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	if fp.filteredIdx != nil {
		t.Fatalf("expected filteredIdx to be nil after Show(), got %v", fp.filteredIdx)
	}
}

func TestFilePickerSuggestionsAppearOnTyping(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "beta"), 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	// Type a character to trigger suggestions
	fp.input.SetValue(fp.inputBasePath() + "a")
	fp.handlePathInput(fp.input.Value())

	if fp.filteredIdx == nil {
		t.Fatalf("expected filteredIdx to be non-nil after typing")
	}
	if len(fp.filteredIdx) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(fp.filteredIdx))
	}
}

func TestFilePickerDownArrowMovesCursor(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "arrow"), 0o755); err != nil {
		t.Fatalf("mkdir arrow: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	// Type to make suggestions visible (both "alpha" and "arrow" match)
	fp.input.SetValue(fp.inputBasePath() + "a")
	fp.handlePathInput(fp.input.Value())

	if len(fp.filteredIdx) != 2 {
		t.Fatalf("expected 2 filtered entries, got %d", len(fp.filteredIdx))
	}

	// First down arrow moves cursor to 1
	fp.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if fp.cursor != 1 {
		t.Fatalf("expected cursor=1 after first down arrow, got %d", fp.cursor)
	}

	// Second down arrow wraps to 0
	fp.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if fp.cursor != 0 {
		t.Fatalf("expected cursor=0 after second down arrow (wrap), got %d", fp.cursor)
	}
}

func TestFilePickerFiltersWithPrefilledPath(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "beta"), 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	fp.input.SetValue(fp.inputBasePath() + "alp")
	fp.applyFilter()

	if len(fp.filteredIdx) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(fp.filteredIdx))
	}
	entry := fp.entries[fp.filteredIdx[0]]
	if entry.Name() != "alpha" {
		t.Fatalf("expected filtered entry 'alpha', got %q", entry.Name())
	}
}

func TestFilePickerDoesNotStripSimilarPrefix(t *testing.T) {
	fp := NewFilePicker("id", "/tmp", true)
	fp.Show()
	fp.currentPath = "/tmp"
	fp.input.SetValue("/tmp2")
	fp.applyFilter()

	// When input is outside current path, filteredIdx should be nil (no suggestions)
	if fp.filteredIdx != nil {
		t.Fatalf("expected nil filteredIdx when input is outside current path, got %v", fp.filteredIdx)
	}
}

func TestFilePickerTildePathDisplay(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home dir: %v", err)
	}

	fp := NewFilePicker("id", home, true)
	fp.Show()

	base := fp.inputBasePath()
	if !strings.HasPrefix(base, "~/") {
		t.Fatalf("expected inputBasePath() to start with '~/', got %q", base)
	}
	if strings.Contains(base, home) {
		t.Fatalf("expected inputBasePath() to not contain full home path %q, got %q", home, base)
	}

	// A subdirectory of home should also use tilde
	sub := filepath.Join(home, "testsubdir")
	fp2 := NewFilePicker("id2", sub, true)
	fp2.currentPath = sub
	base2 := fp2.inputBasePath()
	if !strings.HasPrefix(base2, "~/") {
		t.Fatalf("expected inputBasePath() for subdir to start with '~/', got %q", base2)
	}
}

func TestFilePickerAddButtonConfirmsCurrentDirectory(t *testing.T) {
	tmp := t.TempDir()
	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	fp.renderLines()
	if len(fp.buttonHits) == 0 {
		t.Fatalf("expected button hits to be populated")
	}

	var hit HitRegion
	found := false
	for _, btn := range fp.buttonHits {
		if btn.ID == "open" {
			hit = btn
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected open button hit region")
	}

	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	newPicker, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatalf("expected command from add button click")
	}
	fp = newPicker
	if fp.visible {
		t.Fatalf("expected file picker to be hidden after add")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed || result.Value != tmp {
		t.Fatalf("unexpected dialog result: confirmed=%v value=%q", result.Confirmed, result.Value)
	}
}

func TestFilePickerEnterBasePathConfirmsCurrentDirectory(t *testing.T) {
	tmp := t.TempDir()
	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	fp.input.SetValue(fp.inputBasePath())
	fp.input.CursorEnd()

	_, cmd := fp.handleEnter()
	if cmd == nil {
		t.Fatalf("expected command from enter on base path")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed || result.Value != tmp {
		t.Fatalf("unexpected dialog result: confirmed=%v value=%q", result.Confirmed, result.Value)
	}
}

func TestFilePickerEnterEditedPathConfirmsTypedDirectory(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()

	fp := NewFilePicker("id", base, true)
	fp.Show()

	fp.input.SetValue(target)
	fp.input.CursorEnd()
	fp.cursor = -1

	_, cmd := fp.handleEnter()
	if cmd == nil {
		t.Fatalf("expected command from enter on edited path")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if !result.Confirmed || result.Value != target {
		t.Fatalf("unexpected dialog result: confirmed=%v value=%q", result.Confirmed, result.Value)
	}
}

func TestFilePickerMouseClickOpensDirectory(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	// Type a character to make suggestions visible
	fp.input.SetValue(fp.inputBasePath() + "c")
	fp.handlePathInput(fp.input.Value())

	fp.renderLines()
	if len(fp.rowHits) == 0 {
		t.Fatalf("expected row hits to be populated after typing")
	}

	hit := fp.rowHits[0].region
	clickX, clickY := testFilePickerScreenCoords(fp, hit)

	newPicker, cmd := fp.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd != nil {
		_ = cmd()
	}
	fp = newPicker

	if fp.currentPath != child {
		t.Fatalf("expected current path %q, got %q", child, fp.currentPath)
	}
	if fp.input.Value() != fp.inputBasePath() {
		t.Fatalf("expected input base path %q, got %q", fp.inputBasePath(), fp.input.Value())
	}
}

func TestFilePickerUpArrowWrapsCursor(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "arrow"), 0o755); err != nil {
		t.Fatalf("mkdir arrow: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.SetSize(120, 40)
	fp.Show()

	// Type to make suggestions visible
	fp.input.SetValue(fp.inputBasePath() + "a")
	fp.handlePathInput(fp.input.Value())

	if len(fp.filteredIdx) != 2 {
		t.Fatalf("expected 2 filtered entries, got %d", len(fp.filteredIdx))
	}

	// Cursor starts at 0; pressing up should wrap to last entry
	fp.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if fp.cursor != 1 {
		t.Fatalf("expected cursor=1 (last entry) after pressing up from 0, got %d", fp.cursor)
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		wantLen  int // expected width after truncation (0 means unchanged)
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			maxWidth: 50,
			wantLen:  0, // unchanged
		},
		{
			name:     "exact width unchanged",
			input:    "hello",
			maxWidth: 5,
			wantLen:  0, // unchanged
		},
		{
			name:     "long string truncated",
			input:    "this is a very long string that exceeds the width",
			maxWidth: 20,
			wantLen:  20,
		},
		{
			name:     "very small maxWidth",
			input:    "hello",
			maxWidth: 3,
			wantLen:  0, // returns original when maxWidth <= 3
		},
		{
			name:     "unicode string",
			input:    "héllo wörld",
			maxWidth: 8,
			wantLen:  8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateToWidth(tt.input, tt.maxWidth)
			resultWidth := lipgloss.Width(result)

			if tt.wantLen == 0 {
				if result != tt.input {
					t.Errorf("expected unchanged string %q, got %q", tt.input, result)
				}
			} else {
				if resultWidth > tt.maxWidth {
					t.Errorf("result width %d exceeds maxWidth %d", resultWidth, tt.maxWidth)
				}
				if !strings.HasSuffix(result, "...") {
					t.Errorf("truncated string should end with '...', got %q", result)
				}
			}
		})
	}
}
