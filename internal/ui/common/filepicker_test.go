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

func TestFilePickerDoesNotAutoNavigateOnPathInput(t *testing.T) {
	fp := NewFilePicker("id", t.TempDir(), true)
	fp.Show()

	start := fp.currentPath
	fp.input.SetValue("/")
	fp.handlePathInput("/")

	if fp.currentPath != start {
		t.Fatalf("expected current path to remain %q, got %q", start, fp.currentPath)
	}

	parent := filepath.Dir(start)
	if parent != start {
		fp.input.SetValue(parent)
		fp.handlePathInput(parent)
		if fp.currentPath != start {
			t.Fatalf("expected current path to remain %q after absolute input, got %q", start, fp.currentPath)
		}
	}
}

func TestFilePickerBackspaceMovesToParent(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", nested, true)
	fp.Show()

	if !fp.handleBackspace() {
		t.Fatalf("expected backspace to be handled")
	}

	parent := filepath.Dir(nested)
	if fp.currentPath != parent {
		t.Fatalf("expected current path %q, got %q", parent, fp.currentPath)
	}

	expectedInput := parent
	if parent != string(os.PathSeparator) && !strings.HasSuffix(parent, string(os.PathSeparator)) {
		expectedInput += string(os.PathSeparator)
	}
	if fp.input.Value() != expectedInput {
		t.Fatalf("expected input %q, got %q", expectedInput, fp.input.Value())
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

	if len(fp.filteredIdx) != len(fp.entries) {
		t.Fatalf("expected full entry list when input is outside current path")
	}
}

func TestFilePickerBackspaceEditsNonCurrentPath(t *testing.T) {
	tmp := t.TempDir()
	other := filepath.Join(tmp, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()
	fp.input.SetValue(other)
	fp.input.CursorEnd()

	if fp.handleBackspace() {
		t.Fatalf("expected backspace to be treated as edit for non-current path")
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

	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

	fp.renderLines()
	if len(fp.rowHits) == 0 {
		t.Fatalf("expected row hits to be populated")
	}

	hit := fp.rowHits[0].region
	dialogX, dialogY, _, _ := fp.dialogBounds(fp.lastContentHeight)
	_, _, contentOffsetX, contentOffsetY := fp.dialogFrame()
	clickX := dialogX + contentOffsetX + hit.X + 1
	clickY := dialogY + contentOffsetY + hit.Y

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

func TestFilePickerBackspaceBasePathMovesToParent(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "one")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", nested, true)
	fp.Show()
	fp.input.SetValue(fp.inputBasePath())
	fp.input.CursorEnd()

	if !fp.handleBackspace() {
		t.Fatalf("expected backspace to navigate to parent")
	}

	if fp.currentPath != tmp {
		t.Fatalf("expected current path %q, got %q", tmp, fp.currentPath)
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
