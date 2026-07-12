package common

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/logging"
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
		Foreground(ColorPrimary()).
		MarginBottom(1)
	prefix := titleStyle.Render(d.title) + "\n\n"

	expectedX := inputCursor.X + 3
	expectedY := inputCursor.Y + lipgloss.Height(prefix) + 1

	if c.X != expectedX || c.Y != expectedY {
		t.Fatalf("unexpected cursor position: got (%d,%d), want (%d,%d)", c.X, c.Y, expectedX, expectedY)
	}
}

func TestDialogCursorPositionFilter(t *testing.T) {
	d := NewAgentPicker([]string{"claude", "codex"})
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
		Foreground(ColorPrimary()).
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

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	t.Logf("Content lines (%d):", len(lines))
	for i, line := range lines {
		t.Logf("  [%d]: %q", i, line)
	}

	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
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

func TestDialogConfirmClickNo(t *testing.T) {
	d := NewConfirmDialog("quit", "Quit?", "Are you sure you want to quit?")
	d.SetSize(80, 24)
	d.Show()

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()

	var noHit dialogOptionHit
	for _, hit := range d.optionHits {
		if hit.optionIndex == 1 {
			noHit = hit
			break
		}
	}
	if noHit.region.Width == 0 {
		t.Fatalf("expected hit region for No option")
	}

	screenX := dialogX + contentOffsetX + noHit.region.X + 1
	screenY := dialogY + contentOffsetY + noHit.region.Y

	msg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(msg)

	if cmd == nil {
		t.Fatalf("expected command from clicking No button, got nil")
	}

	result := cmd()
	dialogResult, ok := result.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", result)
	}
	if dialogResult.ID != "quit" {
		t.Fatalf("expected ID 'quit', got %q", dialogResult.ID)
	}
	if dialogResult.Confirmed {
		t.Fatalf("expected Confirmed=false, got true")
	}
}

func TestDialogInputClickCancel(t *testing.T) {
	d := NewInputDialog("create_workspace", "Create Workspace", "Enter workspace name...")
	d.SetSize(80, 24)
	d.Show()
	d.input.SetValue("feature-1")

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)
	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()

	var cancelHit dialogOptionHit
	for _, hit := range d.optionHits {
		if hit.optionIndex == 1 {
			cancelHit = hit
			break
		}
	}
	if cancelHit.region.Width == 0 {
		t.Fatalf("expected hit region for Cancel option")
	}

	screenX := dialogX + contentOffsetX + cancelHit.region.X + 1
	screenY := dialogY + contentOffsetY + cancelHit.region.Y

	msg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(msg)

	if cmd == nil {
		t.Fatalf("expected command from clicking Cancel button, got nil")
	}

	result := cmd()
	dialogResult, ok := result.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", result)
	}
	if dialogResult.ID != "create_workspace" {
		t.Fatalf("expected ID 'create_workspace', got %q", dialogResult.ID)
	}
	if dialogResult.Confirmed {
		t.Fatalf("expected Confirmed=false, got true")
	}
}

func TestDialogInputLogsDoNotIncludeRawValue(t *testing.T) {
	logPath := initDialogTestLogger(t)
	const secret = "secret-dialog-value-123"

	d := NewInputDialog("create_workspace", "Create Workspace", "Enter workspace name...")
	d.Show()
	d.input.SetValue(secret)

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter to produce a DialogResult command")
	}
	msg := cmd()
	result, ok := msg.(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", msg)
	}
	if result.Value != secret {
		t.Fatalf("DialogResult.Value = %q, want original secret", result.Value)
	}

	content := readDialogTestLog(t, logPath)
	if strings.Contains(content, secret) {
		t.Fatalf("dialog log leaked raw value %q: %s", secret, content)
	}
	if !strings.Contains(content, "value_len=") {
		t.Fatalf("expected dialog log to keep value length metadata, got: %s", content)
	}
}

func initDialogTestLogger(t *testing.T) string {
	t.Helper()

	if err := logging.Initialize(t.TempDir(), logging.LevelDebug); err != nil {
		t.Fatalf("logging.Initialize: %v", err)
	}
	t.Cleanup(func() { _ = logging.Close() })
	return logging.GetLogPath()
}

func readDialogTestLog(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(data)
}

// longMessage returns a message long enough to word-wrap in a dialog with
// the default content width (~70 chars).
func longMessage() string {
	return "Remove project 'manim_magical_6mo_epics' from AMUX? This won't delete any files."
}

func TestDialogConfirmClickYesWrappingMessage(t *testing.T) {
	d := NewConfirmDialog("remove", "Remove Project", longMessage())
	d.SetSize(80, 24)
	d.Show()

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)

	t.Logf("Content lines (%d):", len(lines))
	for i, line := range lines {
		t.Logf("  [%d]: %q", i, line)
	}

	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	t.Logf("Dialog bounds: x=%d, y=%d, w=%d, h=%d", dialogX, dialogY, dialogW, dialogH)

	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()
	t.Logf("Frame offsets: contentOffsetX=%d, contentOffsetY=%d", contentOffsetX, contentOffsetY)

	t.Logf("Option hits (%d):", len(d.optionHits))
	for i, hit := range d.optionHits {
		t.Logf("  [%d]: cursorIdx=%d optionIdx=%d region=(%d,%d,%d,%d)",
			i, hit.cursorIndex, hit.optionIndex, hit.region.X, hit.region.Y, hit.region.Width, hit.region.Height)
	}

	// Verify the message actually wraps (otherwise this test isn't exercising the fix)
	renderedCount := d.renderedLineCount(lines[:len(lines)-1]) // lines before options
	rawCount := len(lines) - 1
	t.Logf("Raw pre-option lines: %d, Rendered pre-option lines: %d", rawCount, renderedCount)
	if renderedCount <= rawCount {
		t.Logf("WARNING: message did not wrap; test may not exercise the wrapping fix")
	}

	var yesHit dialogOptionHit
	for _, hit := range d.optionHits {
		if hit.optionIndex == 0 {
			yesHit = hit
			break
		}
	}

	screenX := dialogX + contentOffsetX + yesHit.region.X + 1
	screenY := dialogY + contentOffsetY + yesHit.region.Y
	t.Logf("Clicking at screen (%d,%d) for Yes button", screenX, screenY)

	msg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(msg)

	if cmd == nil {
		t.Fatalf("Expected command from clicking Yes button, got nil")
	}

	result := cmd()
	dialogResult, ok := result.(DialogResult)
	if !ok {
		t.Fatalf("Expected DialogResult, got %T", result)
	}
	if dialogResult.ID != "remove" {
		t.Fatalf("Expected ID 'remove', got %q", dialogResult.ID)
	}
	if !dialogResult.Confirmed {
		t.Fatalf("Expected Confirmed=true, got false")
	}
}

func TestDialogConfirmClickNoWrappingMessage(t *testing.T) {
	d := NewConfirmDialog("remove", "Remove Project", longMessage())
	d.SetSize(80, 24)
	d.Show()

	lines := d.renderLines()
	content := strings.Join(lines, "\n")
	dialogView := d.dialogStyle().Render(content)
	dialogW, dialogH := viewDimensions(dialogView)

	dialogX := (d.width - dialogW) / 2
	dialogY := (d.height - dialogH) / 2
	if dialogX < 0 {
		dialogX = 0
	}
	if dialogY < 0 {
		dialogY = 0
	}
	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()

	var noHit dialogOptionHit
	for _, hit := range d.optionHits {
		if hit.optionIndex == 1 {
			noHit = hit
			break
		}
	}
	if noHit.region.Width == 0 {
		t.Fatalf("expected hit region for No option")
	}

	screenX := dialogX + contentOffsetX + noHit.region.X + 1
	screenY := dialogY + contentOffsetY + noHit.region.Y

	clickMsg := tea.MouseClickMsg{X: screenX, Y: screenY, Button: tea.MouseLeft}
	_, cmd := d.Update(clickMsg)

	if cmd == nil {
		t.Fatalf("Expected command from clicking No button, got nil")
	}

	result := cmd()
	dialogResult, ok := result.(DialogResult)
	if !ok {
		t.Fatalf("Expected DialogResult, got %T", result)
	}
	if dialogResult.ID != "remove" {
		t.Fatalf("Expected ID 'remove', got %q", dialogResult.ID)
	}
	if dialogResult.Confirmed {
		t.Fatalf("Expected Confirmed=false, got true")
	}
}

func TestDialogSetInputValuePrefills(t *testing.T) {
	d := NewInputDialog("rename", "Rename workspace", "Enter new name...")
	// Show() resets the input to empty, so the prefill must land after it —
	// mirroring how the rename dialog is wired (present, then prefill).
	d.Show()
	d.SetInputValue("existing-name")

	if got := d.input.Value(); got != "existing-name" {
		t.Fatalf("SetInputValue prefill = %q, want %q", got, "existing-name")
	}

	// Confirming emits the prefilled value as the DialogResult value.
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected a command from confirming the input dialog")
	}
	result, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", cmd())
	}
	if result.Value != "existing-name" {
		t.Fatalf("DialogResult.Value = %q, want %q", result.Value, "existing-name")
	}
}

func TestDialogSetInputValueIgnoredForNonInput(t *testing.T) {
	// A no-op on confirm dialogs: it must not panic or mutate anything.
	d := NewConfirmDialog("confirm", "Title", "Message")
	d.SetInputValue("ignored")
	if got := d.input.Value(); got != "" {
		t.Fatalf("confirm dialog input value = %q, want empty", got)
	}
}
