package common

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestDialogCursorHiddenWhenNotVisible(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	if c := d.Cursor(); c != nil {
		t.Fatalf("expected nil cursor when dialog is hidden, got %+v", c)
	}
}

func TestDialogCursorPositionInput(t *testing.T) {
	d := NewInputDialog("id", "Title", "Placeholder")
	d.Show()
	d.input.SetValue("abc")
	d.input.SetCursor(3)

	inputCursor := d.input.Cursor()
	if inputCursor == nil {
		t.Fatalf("expected input cursor, got nil")
	}

	c := d.Cursor()
	if c == nil {
		t.Fatalf("expected dialog cursor, got nil")
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	prefix := titleStyle.Render(d.title) + "\n\n"

	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 2

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}

func TestDialogCursorPositionFilter(t *testing.T) {
	d := NewAgentPicker()
	d.Show()
	d.filterInput.SetValue("c")
	d.filterInput.SetCursor(1)

	inputCursor := d.filterInput.Cursor()
	if inputCursor == nil {
		t.Fatalf("expected filter input cursor, got nil")
	}

	c := d.Cursor()
	if c == nil {
		t.Fatalf("expected dialog cursor, got nil")
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)
	prefix := titleStyle.Render(d.title) + "\n\n"
	if d.message != "" {
		prefix += d.message + "\n\n"
	}

	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 2

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}
