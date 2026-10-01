package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newUIDialog builds a dialog with the Interface section populated — the
// three formerly JSON-only ui.* keys.
func newUIDialog() *SettingsDialog {
	d := NewSettingsDialog(ThemeGruvbox, "", "", "")
	d.SetUIOptions(true, false, "code --wait")
	d.Show()
	return d
}

// The Interface section renders all three rows and focuses them in enum
// order between the tmux fields and Assistants.
func TestSettingsInterfaceRowsRenderAndNavigate(t *testing.T) {
	d := newUIDialog()
	view := ansi.Strip(d.View())
	for _, want := range []string{"Interface", "Keybinding hints: on", "Bell when agent finishes: off", "Viewer command: code --wait"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for i, want := range []settingsItem{
		settingsItemKeymapHints, settingsItemNotifyOnDone, settingsItemViewerCmd, settingsItemAssistants,
	} {
		for j := 0; j <= i; j++ {
		}
		_ = i
		_ = want
	}
	// Theme→Tmux×3→Interface×3→Assistants = 7 tabs.
	for i := 0; i < 7; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("7 tabs should land on Assistants, got %d", d.focusedItem)
	}
}

// Enter toggles the bool rows in place without leaving the section.
func TestSettingsInterfaceToggles(t *testing.T) {
	d := newUIDialog()
	for i := 0; i < 4; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if d.focusedItem != settingsItemKeymapHints {
		t.Fatalf("want keymap hints focused, got %d", d.focusedItem)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.ShowKeymapHints() != false {
		t.Fatal("enter should toggle keymap hints off")
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !d.ShowKeymapHints() {
		t.Fatal("second enter should toggle back on")
	}
	// Space toggles too (handleSelect is bound to enter+space).
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !d.NotifyOnDone() {
		t.Fatal("space should toggle notify_on_done on")
	}
}

// The viewer-command row is a text field: printable input edits it,
// backspace deletes, and structural keys still move focus.
func TestSettingsViewerCommandField(t *testing.T) {
	d := newUIDialog()
	for i := 0; i < 6; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if d.focusedItem != settingsItemViewerCmd {
		t.Fatalf("want viewer command focused, got %d", d.focusedItem)
	}
	// j/k are text here, not navigation.
	for _, r := range " -n jk" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := d.ViewerCommand(); got != "code --wait -n jk" {
		t.Fatalf("viewer command = %q", got)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := d.ViewerCommand(); got != "code --wait -n j" {
		t.Fatalf("backspace = %q", got)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("down should leave the field, got %d", d.focusedItem)
	}
}

// Paste lands in the viewer command field (first line only, filtered to
// printable runes — the shared single-line-field contract).
func TestSettingsViewerCommandPaste(t *testing.T) {
	d := newUIDialog()
	for i := 0; i < 6; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	d.Update(tea.PasteMsg{Content: " -g\nrm -rf /"})
	if got := d.ViewerCommand(); got != "code --wait -g" {
		t.Fatalf("paste should take first line only, got %q", got)
	}
}

// Shift+Tab backwards from Close skips a hidden Update row and lands on
// Assistants — the interface rows precede it but are not skipped.
func TestSettingsInterfaceNavBackward(t *testing.T) {
	d := newUIDialog()
	d.focusedItem = settingsItemClose
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if d.focusedItem != settingsItemAssistants {
		t.Fatalf("shift+tab from close should land on Assistants, got %d", d.focusedItem)
	}
}
