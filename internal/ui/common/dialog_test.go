package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 1

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
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 1

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}

func TestDialogConfirmClickYes(t *testing.T) {
	d := NewConfirmDialog("quit", "Quit?", "Are you sure you want to quit?")
	d.SetSize(80, 24)
	d.Show()

	// First render to populate optionHits
	_ = d.View()
	lines := d.renderLines()
	t.Logf("Content lines (%d):", len(lines))
	for i, line := range lines {
		t.Logf("  [%d]: %q", i, line)
	}

	// Get dialog bounds
	contentHeight := len(lines)
	dialogX, dialogY, dialogW, dialogH := d.dialogBounds(contentHeight)
	t.Logf("Dialog bounds: x=%d, y=%d, w=%d, h=%d", dialogX, dialogY, dialogW, dialogH)

	frameX, frameY, contentOffsetX, contentOffsetY := d.dialogFrame()
	t.Logf("Frame: x=%d, y=%d, offsetX=%d, offsetY=%d", frameX, frameY, contentOffsetX, contentOffsetY)

	t.Logf("Option hits (%d):", len(d.optionHits))
	for i, hit := range d.optionHits {
		t.Logf("  [%d]: cursorIdx=%d optionIdx=%d region=(%d,%d,%d,%d)",
			i, hit.cursorIndex, hit.optionIndex, hit.region.X, hit.region.Y, hit.region.Width, hit.region.Height)
	}

	// Find the "Yes" button hit region (optionIndex=0)
	var yesHit dialogOptionHit
	for _, hit := range d.optionHits {
		if hit.optionIndex == 0 {
			yesHit = hit
			break
		}
	}

	// Calculate screen coordinates for clicking "Yes"
	// screenX = dialogX + contentOffsetX + localX
	// screenY = dialogY + contentOffsetY + localY
	screenX := dialogX + contentOffsetX + yesHit.region.X + 1 // +1 to be inside the button
	screenY := dialogY + contentOffsetY + yesHit.region.Y
	t.Logf("Clicking at screen (%d,%d) for Yes button at local (%d,%d)", screenX, screenY, yesHit.region.X, yesHit.region.Y)

	// Send click
	msg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(msg)

	if cmd == nil {
		t.Fatalf("Expected command from clicking Yes button, got nil")
	}

	// Execute the command and check the result
	result := cmd()
	dialogResult, ok := result.(DialogResult)
	if !ok {
		t.Fatalf("Expected DialogResult, got %T", result)
	}
	if dialogResult.ID != "quit" {
		t.Fatalf("Expected ID 'quit', got %q", dialogResult.ID)
	}
	if !dialogResult.Confirmed {
		t.Fatalf("Expected Confirmed=true, got false")
	}
}
