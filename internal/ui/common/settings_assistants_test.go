package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSettingsDialogEditsAssistantCommand mirrors
// TestSettingsDialogEditsTmuxFields: it drives the dialog's real key router
// (Update) rather than poking fields directly, so it also exercises
// navigation into and within the Assistants section.
func TestSettingsDialogEditsAssistantCommand(t *testing.T) {
	typeInto := func(d *SettingsDialog, s string) {
		for _, r := range s {
			d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}

	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude", "codex"}, map[string]string{
		"claude": "claude",
		"codex":  "codex",
	})
	d.Show()

	// Tab from Theme through the three tmux fields lands on Assistants.
	for range 4 {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("focusedItem after 4 tabs = %d, want settingsItemAssistants", d.focusedItem)
	}
	if d.assistantCursor != 0 {
		t.Fatalf("assistantCursor = %d, want 0 (claude, the first row)", d.assistantCursor)
	}

	// Typing (including letters like j/k, mirroring the tmux-field contract)
	// edits the focused assistant's command, not list navigation.
	typeInto(d, " --resume jk")
	if got := d.AssistantCommands()["claude"]; got != "claude --resume jk" {
		t.Errorf("claude command = %q, want %q", got, "claude --resume jk")
	}

	// Down moves the cursor to the next assistant row without leaving the
	// section.
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("focusedItem after Down = %d, want settingsItemAssistants (stay in section)", d.focusedItem)
	}
	if d.assistantCursor != 1 {
		t.Fatalf("assistantCursor after Down = %d, want 1 (codex)", d.assistantCursor)
	}

	typeInto(d, "-extra")
	if got := d.AssistantCommands()["codex"]; got != "codex-extra" {
		t.Errorf("codex command = %q, want %q", got, "codex-extra")
	}
	// The other row's command must be untouched.
	if got := d.AssistantCommands()["claude"]; got != "claude --resume jk" {
		t.Errorf("claude command changed unexpectedly: %q", got)
	}

	// Backspace deletes the last rune of the focused (codex) command.
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := d.AssistantCommands()["codex"]; got != "codex-extr" {
		t.Errorf("codex command after backspace = %q, want %q", got, "codex-extr")
	}

	// Down again wraps back around to the first row (only two assistants).
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.assistantCursor != 0 {
		t.Fatalf("assistantCursor after wrapping Down = %d, want 0", d.assistantCursor)
	}

	// Up from the first row wraps to the last.
	d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if d.assistantCursor != 1 {
		t.Fatalf("assistantCursor after wrapping Up = %d, want 1", d.assistantCursor)
	}

	// Tab leaves the section (no update available, so it lands on Close).
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if d.focusedItem != settingsItemClose {
		t.Fatalf("focusedItem after Tab out of Assistants = %d, want settingsItemClose", d.focusedItem)
	}
}

// TestSettingsDialogAssistantFieldEscStillCancels confirms Esc is handled
// globally before the Assistants field router, matching the tmux fields'
// contract (Esc always cancels the whole dialog, whatever is focused).
func TestSettingsDialogAssistantFieldEscStillCancels(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude"}, map[string]string{"claude": "claude"})
	d.Show()
	d.focusedItem = settingsItemAssistants

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.Visible() {
		t.Fatal("esc should hide the dialog even while an assistant field is focused")
	}
	result, ok := cmd().(SettingsResult)
	if !ok || !result.Canceled {
		t.Fatalf("expected canceled SettingsResult, got %#v (ok=%v)", cmd(), ok)
	}
}

// TestSettingsDialogAssistantFieldEmptyRosterIsNoop guards the zero-roster
// edge case (a dialog built without SetAssistants, as most existing tests
// do): navigating into/around the Assistants section and typing must not
// panic or mutate a nil map.
func TestSettingsDialogAssistantFieldEmptyRosterIsNoop(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.Show()
	d.focusedItem = settingsItemAssistants

	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	if len(d.AssistantCommands()) != 0 {
		t.Fatalf("expected no assistant commands for an empty roster, got %#v", d.AssistantCommands())
	}
}

// TestSettingsRenderAssistantsSection confirms renderLines lists every
// roster entry with its (possibly edited) command, and highlights the
// cursor's row when the section is focused.
func TestSettingsRenderAssistantsSection(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude", "codex"}, map[string]string{
		"claude": "claude --resume",
		"codex":  "codex",
	})
	d.focusedItem = settingsItemAssistants
	d.assistantCursor = 1

	joined := strings.Join(d.renderLines(), "\n")
	if !strings.Contains(joined, "Assistants") {
		t.Fatalf("expected an Assistants section header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "claude: claude --resume") {
		t.Fatalf("expected claude's row with its command, got:\n%s", joined)
	}
	if !strings.Contains(joined, "codex: codex") {
		t.Fatalf("expected codex's row with its command, got:\n%s", joined)
	}
}

// TestSettingsRenderAssistantsSectionHiddenWhenEmpty confirms a dialog with
// no roster set (the common case in tests that predate SetAssistants) does
// not render an empty "Assistants" header with nothing under it.
func TestSettingsRenderAssistantsSectionHiddenWhenEmpty(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	joined := strings.Join(d.renderLines(), "\n")
	if strings.Contains(joined, "Assistants") {
		t.Fatalf("expected no Assistants section without a roster, got:\n%s", joined)
	}
}

// TestSettingsViewScrollsAssistantCursorIntoView exercises the same
// 043 scroll machinery TestSettingsViewScrollsThemeCursorIntoView exercises
// for the theme cursor, but for assistantCursor: focusing a late row in a
// long roster at a short dialog height must still scroll it into view, and
// [Close] must remain visible throughout (see focusedBodyIndex's
// settingsItemAssistants case in settings_scroll.go).
func TestSettingsViewScrollsAssistantCursorIntoView(t *testing.T) {
	names := make([]string, 20)
	commands := make(map[string]string, 20)
	for i := range names {
		names[i] = "assistant-" + string(rune('a'+i))
		commands[names[i]] = "cmd-" + string(rune('a'+i))
	}

	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants(names, commands)
	d.SetSize(120, 15)
	d.Show()
	d.focusedItem = settingsItemAssistants
	d.assistantCursor = len(names) - 1 // far below a 15-row dialog's window

	view := d.View()
	lastName := names[len(names)-1]
	if !strings.Contains(view, lastName) {
		t.Fatalf("expected focused assistant %q to be scrolled into view, got:\n%s", lastName, view)
	}
	if !strings.Contains(view, "[Close]") {
		t.Fatal("expected [Close] to remain visible while the body is scrolled")
	}
}

// TestSettingsClickOnAssistantRowFocusesAndSetsCursor confirms clicking an
// assistant row (via handleClick, the same path composeVisibleLines/
// remapHitRegions feed) both focuses the Assistants section and selects
// that row's cursor index -- mirroring how a theme click sets themeCursor.
func TestSettingsClickOnAssistantRowFocusesAndSetsCursor(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude", "codex"}, map[string]string{
		"claude": "claude",
		"codex":  "codex",
	})
	d.SetSize(120, 40)
	d.Show()

	lines := d.composeVisibleLines()
	contentHeight := len(lines)
	dialogX, dialogY, _, _ := d.dialogBounds(contentHeight)
	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()

	// Find codex's on-screen row via the hit regions composeVisibleLines just
	// remapped, rather than hard-coding a row offset that would silently
	// drift if the section layout changes.
	codexY := -1
	for _, hit := range d.hitRegions {
		if hit.item == settingsItemAssistants && hit.index == 1 {
			codexY = hit.region.Y
		}
	}
	if codexY < 0 {
		t.Fatal("expected a hit region for the codex assistant row")
	}

	msg := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      dialogX + contentOffsetX,
		Y:      dialogY + contentOffsetY + codexY,
	}
	d.handleClick(msg)

	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("focusedItem after click = %d, want settingsItemAssistants", d.focusedItem)
	}
	if d.assistantCursor != 1 {
		t.Fatalf("assistantCursor after click = %d, want 1 (codex)", d.assistantCursor)
	}
}
