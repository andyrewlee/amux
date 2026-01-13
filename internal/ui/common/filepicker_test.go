package common

import (
	"testing"

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

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	pathStyle := lipgloss.NewStyle().
		Foreground(ColorSecondary)

	prefix := titleStyle.Render(fp.title) + "\n\n" + pathStyle.Render(fp.currentPath) + "\n\n"
	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 2

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}
