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

// typeIntoAssistant drives the dialog's real key router while the Assistants
// section is focused (mirrors typeIntoEnvDialog).
func typeIntoAssistant(d *SettingsDialog, s string) {
	for _, r := range s {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestSettingsDialogAssistantAddFirstEntry covers the empty-roster case: the
// section renders its "ctrl+a to add" affordance and the first assistant is
// created entirely through the two-field add input.
func TestSettingsDialogAssistantAddFirstEntry(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants(nil, nil)
	d.Show()
	d.focusedItem = settingsItemAssistants

	// The empty roster still renders the section + affordance.
	joined := strings.Join(d.renderLines(), "\n")
	if !strings.Contains(joined, "ctrl+a to add") {
		t.Fatalf("empty roster should render the add affordance, got:\n%s", joined)
	}

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !d.assistantAdding {
		t.Fatal("ctrl+a did not open the add input")
	}
	typeIntoAssistant(d, "Gemini")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 1 {
		t.Fatalf("enter on a valid name did not advance to command (field=%d)", d.assistantAddField)
	}
	typeIntoAssistant(d, "gemini --fast")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.assistantAdding {
		t.Fatal("commit did not exit add mode")
	}
	if got := d.AssistantCommands()["gemini"]; got != "gemini --fast" {
		t.Fatalf("AssistantCommands()[gemini] = %q, want %q", got, "gemini --fast")
	}
	if len(d.assistantNames) != 1 || d.assistantNames[0] != "gemini" {
		t.Fatalf("assistantNames = %#v, want [gemini] (name normalized lowercase)", d.assistantNames)
	}
	if d.assistantCursor != 0 {
		t.Fatalf("assistantCursor = %d, want 0 (the new row)", d.assistantCursor)
	}
}

// TestSettingsDialogAssistantAddDuplicate pins the duplicate contract: a
// normalized-name collision cancels the add, focuses the existing row, and
// never overwrites its command.
func TestSettingsDialogAssistantAddDuplicate(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude"}, map[string]string{"claude": "claude"})
	d.Show()
	d.focusedItem = settingsItemAssistants

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoAssistant(d, "CLAUDE")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeIntoAssistant(d, "different-cmd")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.assistantAdding {
		t.Fatal("duplicate add should exit add mode")
	}
	if got := d.AssistantCommands()["claude"]; got != "claude" {
		t.Fatalf("duplicate add overwrote the command: %q, want %q", got, "claude")
	}
	if d.assistantCursor != 0 {
		t.Fatalf("assistantCursor = %d, want 0 (the existing claude row)", d.assistantCursor)
	}
	if d.assistantNotice == "" {
		t.Fatal("duplicate add should leave a notice")
	}
	if len(d.assistantNames) != 1 {
		t.Fatalf("assistantNames = %#v — duplicate must not append", d.assistantNames)
	}
}

// TestSettingsDialogAssistantAddValidation covers name and command rules.
func TestSettingsDialogAssistantAddValidation(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants(nil, nil)
	d.Show()
	d.focusedItem = settingsItemAssistants

	// Empty name stays on the name field with an error.
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 0 || d.assistantAddError == "" {
		t.Fatalf("empty name should stay on name field with error (field=%d err=%q)", d.assistantAddField, d.assistantAddError)
	}

	// Whitespace inside the name is rejected.
	d.assistantAddName = "two words"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 0 || d.assistantAddError == "" {
		t.Fatalf("whitespace name should stay on name field with error (field=%d err=%q)", d.assistantAddField, d.assistantAddError)
	}

	// Valid name advances; empty command blocks the commit.
	d.assistantAddName = "newbot"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 1 {
		t.Fatalf("valid name should advance (field=%d err=%q)", d.assistantAddField, d.assistantAddError)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !d.assistantAdding || d.assistantAddError == "" {
		t.Fatalf("empty command should keep add mode open with error (adding=%v err=%q)", d.assistantAdding, d.assistantAddError)
	}
}

// TestSettingsDialogAssistantAddNameValidator confirms the domain hook wired
// via SetAssistantNameValidator rejects names the generic rules accept —
// the path that keeps config-load validation from silently dropping a
// dialog-added assistant on restart.
func TestSettingsDialogAssistantAddNameValidator(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants(nil, nil)
	d.SetAssistantNameValidator(func(name string) string {
		if name == "bad!name" {
			return "assistant must start with letter/number and contain only letters, numbers, dots, dashes, or underscores"
		}
		return ""
	})
	d.Show()
	d.focusedItem = settingsItemAssistants

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoAssistant(d, "bad!name")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 0 || !strings.Contains(d.assistantAddError, "letter/number") {
		t.Fatalf("validator-rejected name should stay on name field with its error (field=%d err=%q)", d.assistantAddField, d.assistantAddError)
	}

	d.assistantAddName = "goodname"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.assistantAddField != 1 {
		t.Fatalf("accepted name should advance (field=%d err=%q)", d.assistantAddField, d.assistantAddError)
	}
}

// TestSettingsDialogAssistantAddCancel pins the Esc contract inside add mode:
// it cancels only the add — the dialog stays open and nothing is appended.
func TestSettingsDialogAssistantAddCancel(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants([]string{"claude"}, map[string]string{"claude": "claude"})
	d.Show()
	d.focusedItem = settingsItemAssistants

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoAssistant(d, "partial")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if d.assistantAdding {
		t.Fatal("esc did not cancel add mode")
	}
	if !d.Visible() {
		t.Fatal("esc inside add mode must not close the dialog")
	}
	if len(d.assistantNames) != 1 {
		t.Fatalf("assistantNames = %#v — canceled add must not append", d.assistantNames)
	}

	// Esc on the list still cancels the whole dialog.
	d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.Visible() {
		t.Fatal("esc on the list should close the dialog")
	}
}
