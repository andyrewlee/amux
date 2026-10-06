package common

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestEnvDialogSanitizesStoredValuesForDisplay: stored env bytes can carry
// SGR escapes and newlines (a hostile or careless value persisted earlier);
// the dialog must strip them from the rendered frame while leaving the
// stored map untouched for the caller that persists it.
func TestEnvDialogSanitizesStoredValuesForDisplay(t *testing.T) {
	raw := "safe\x1b[31m\nINJECTED"
	d := NewEnvDialog(map[string]string{
		"EV\x1b[7mIL": raw,
		"PLAIN":       "ok",
	})
	d.Show()

	view := d.View()
	if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\x1b[7m") {
		t.Fatalf("stored escape bytes reached the frame:\n%q", view)
	}
	if strings.Contains(view, "safe\nINJECTED") {
		t.Fatalf("stored newline split a rendered row:\n%q", view)
	}
	if !strings.Contains(view, "EVIL: safeINJECTED") {
		t.Fatalf("expected sanitized row 'EVIL: safeINJECTED', got:\n%s", view)
	}

	// The stored bytes must survive verbatim — display sanitization is a
	// copy-side concern, not a mutation.
	if got := d.Env()["EV\x1b[7mIL"]; got != raw {
		t.Fatalf("Env() = %q, want the raw stored value %q", got, raw)
	}
}

// TestEnvDialogSanitizesAddFields: text typed into the add-mode fields gets
// the same display treatment — the compositor must never see raw input.
func TestEnvDialogSanitizesAddFields(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	d.Show()
	d.startAdd()
	d.addName = "NA\x1b[31mME"
	d.addValue = "va\nl"
	d.addField = 1

	view := d.View()
	if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "va\nl") {
		t.Fatalf("add-field control bytes reached the frame:\n%q", view)
	}
	if !strings.Contains(view, "name:  NAME") || !strings.Contains(view, "value: val") {
		t.Fatalf("expected sanitized add fields, got:\n%s", view)
	}
	// addName/addValue themselves keep the raw bytes — validation owns them.
	if d.addName != "NA\x1b[31mME" || d.addValue != "va\nl" {
		t.Fatalf("add fields were mutated: name=%q value=%q", d.addName, d.addValue)
	}
}

// TestEnvDialogScrollWindowClampsHeight: with a frame height smaller than
// the content, View() renders a bounded window — header + visible body +
// footer — instead of every row.
func TestEnvDialogScrollWindowClampsHeight(t *testing.T) {
	env := make(map[string]string, 20)
	for i := 0; i < 20; i++ {
		env[fmt.Sprintf("KEY%02d", i)] = fmt.Sprintf("v%d", i)
	}
	d := NewEnvDialog(env)
	d.Show()
	d.SetSize(60, 14) // frame 4 + header 2 + footer 2 leaves a 6-row window

	_, height := viewDimensions(d.View())
	if height > 14 {
		t.Fatalf("View height = %d, want <= the 14-row frame", height)
	}
	if !strings.Contains(d.View(), "enter save") {
		t.Fatalf("footer hint must stay pinned, got:\n%s", d.View())
	}
}

// TestEnvDialogScrollFollowsCursor: moving the cursor past the window edge
// scrolls the body so the focused row stays visible.
func TestEnvDialogScrollFollowsCursor(t *testing.T) {
	env := make(map[string]string, 20)
	for i := 0; i < 20; i++ {
		env[fmt.Sprintf("KEY%02d", i)] = fmt.Sprintf("v%d", i)
	}
	d := NewEnvDialog(env)
	d.Show()
	d.SetSize(60, 14)

	if !strings.Contains(d.View(), "KEY00") {
		t.Fatal("first row should be visible at offset 0")
	}
	for i := 0; i < 19; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := d.View()
	if !strings.Contains(view, "KEY19") {
		t.Fatalf("focused bottom row scrolled out of view:\n%s", view)
	}
	if strings.Contains(view, "KEY00") {
		t.Fatalf("top row should have scrolled away:\n%s", view)
	}
}

// TestEnvDialogScrollKeepsAddFieldsReachable: ctrl+a at the bottom of a long
// list must leave the add fields inside the window, on both fields.
func TestEnvDialogScrollKeepsAddFieldsReachable(t *testing.T) {
	env := make(map[string]string, 20)
	for i := 0; i < 20; i++ {
		env[fmt.Sprintf("KEY%02d", i)] = fmt.Sprintf("v%d", i)
	}
	d := NewEnvDialog(env)
	d.Show()
	d.SetSize(60, 14)

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !d.adding {
		t.Fatal("ctrl+a did not enter add mode")
	}
	if view := d.View(); !strings.Contains(view, "name:") {
		t.Fatalf("add name field scrolled out of view:\n%s", view)
	}
	d.addName = "NEWKEY"
	d.addField = 1
	if view := d.View(); !strings.Contains(view, "value:") {
		t.Fatalf("add value field scrolled out of view:\n%s", view)
	}
}

// TestEnvDialogUnsizedHeightIsUnbounded: tests and callers that never call
// SetSize keep the render-everything behavior.
func TestEnvDialogUnsizedHeightIsUnbounded(t *testing.T) {
	env := make(map[string]string, 20)
	for i := 0; i < 20; i++ {
		env[fmt.Sprintf("KEY%02d", i)] = fmt.Sprintf("v%d", i)
	}
	d := NewEnvDialog(env)
	d.Show()
	view := d.View()
	if !strings.Contains(view, "KEY00") || !strings.Contains(view, "KEY19") {
		t.Fatalf("unbounded render must show every row:\n%s", view)
	}
}
