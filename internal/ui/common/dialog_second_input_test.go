package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func typeRunes(t *testing.T, d *Dialog, s string) *Dialog {
	t.Helper()
	for _, r := range s {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return d
}

func pressKey(t *testing.T, d *Dialog, msg tea.KeyPressMsg) (*Dialog, tea.Cmd) {
	t.Helper()
	return d.Update(msg)
}

func tab() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyTab} }
func shiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }
func enter() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyEnter} }

// TestDialogSecondInputRouting proves keystrokes land on whichever field owns
// focus and that tab/shift-tab toggle it in both directions.
func TestDialogSecondInputRouting(t *testing.T) {
	d := NewInputDialog("id", "Title", "name hint")
	d.SetInputLabel("Name")
	d.SetSecondInput("Base", "base hint", nil)
	d.Show()

	d = typeRunes(t, d, "alpha")
	if got := d.input.Value(); got != "alpha" {
		t.Fatalf("first field got %q", got)
	}
	if got := d.input2.Value(); got != "" {
		t.Fatalf("second field received input while unfocused: %q", got)
	}

	d, _ = pressKey(t, d, tab())
	if !d.focused2 {
		t.Fatal("tab did not move focus to the second field")
	}
	d = typeRunes(t, d, "release/1.2")
	if got := d.input2.Value(); got != "release/1.2" {
		t.Fatalf("second field got %q", got)
	}
	if got := d.input.Value(); got != "alpha" {
		t.Fatalf("first field mutated while unfocused: %q", got)
	}

	d, _ = pressKey(t, d, shiftTab())
	if d.focused2 {
		t.Fatal("shift+tab did not return focus to the first field")
	}
	d = typeRunes(t, d, "x")
	if got := d.input.Value(); got != "alphax" {
		t.Fatalf("first field got %q after refocus", got)
	}
}

// TestDialogSecondInputCursorRow proves the reported caret row lands on the
// focused field — moving focus to the second input must advance the row by
// exactly the rows the first field, its label, and the second label occupy.
func TestDialogSecondInputCursorRow(t *testing.T) {
	d := NewInputDialog("id", "Title", "name hint")
	d.SetInputLabel("Name")
	d.SetSecondInput("Base", "base hint", nil)
	d.Show()

	first := d.Cursor()
	if first == nil {
		t.Fatal("expected a cursor for the focused first field")
	}
	d, _ = pressKey(t, d, tab())
	second := d.Cursor()
	if second == nil {
		t.Fatal("expected a cursor for the focused second field")
	}

	// Rows between the two inputs: field-1 input view + field-2 label (the
	// first label sits above field 1, so it is not part of the delta). No
	// validation errors render in this fixture.
	rowsBetween := lipgloss.Height(d.input.View()) +
		lipgloss.Height(d.fieldLabel(d.input2Label, true))
	if got := second.Y - first.Y; got != rowsBetween {
		t.Fatalf("caret advanced %d rows between fields, want %d", got, rowsBetween)
	}
	if second.X != first.X {
		t.Fatalf("caret column changed across fields: %d vs %d", first.X, second.X)
	}
}

// TestDialogSecondInputResultValue2 proves Enter on either field confirms once
// and reports both values.
func TestDialogSecondInputResultValue2(t *testing.T) {
	run := func(t *testing.T, confirmOnSecond bool) DialogResult {
		t.Helper()
		d := NewInputDialog("id", "Title", "name hint").SetSecondInput("Base", "hint", nil)
		d.Show()
		d = typeRunes(t, d, "ws-one")
		d, _ = pressKey(t, d, tab())
		d = typeRunes(t, d, "main")
		if !confirmOnSecond {
			d, _ = pressKey(t, d, shiftTab())
		}
		_, cmd := pressKey(t, d, enter())
		if cmd == nil {
			t.Fatal("enter produced no command")
		}
		res, ok := cmd().(DialogResult)
		if !ok {
			t.Fatalf("expected DialogResult, got %T", cmd())
		}
		return res
	}

	for _, confirmOnSecond := range []bool{false, true} {
		res := run(t, confirmOnSecond)
		if !res.Confirmed || res.Value != "ws-one" || res.Value2 != "main" {
			t.Fatalf("confirmOnSecond=%v got %+v", confirmOnSecond, res)
		}
	}
}

// TestDialogSecondInputValidation proves the second field's validator blocks
// Enter from either field and reports into its own error slot — without
// disturbing the first field's error state.
func TestDialogSecondInputValidation(t *testing.T) {
	notBad := func(s string) string {
		if strings.Contains(s, "bad") {
			return "base is bad"
		}
		return ""
	}

	t.Run("invalid second field blocks enter and shows error", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint").SetSecondInput("Base", "hint", notBad)
		d.Show()
		d = typeRunes(t, d, "ws")
		d, _ = pressKey(t, d, tab())
		d = typeRunes(t, d, "bad..ref")

		if _, cmd := pressKey(t, d, enter()); cmd != nil {
			t.Fatal("enter accepted while the second field was invalid")
		}
		if !d.Visible() {
			t.Fatal("dialog closed despite invalid second field")
		}
		if d.validationErr2 == "" {
			t.Fatal("second field error not recorded")
		}
		if d.validationErr != "" {
			t.Fatalf("first field error polluted by second field: %q", d.validationErr)
		}
		if view := ansi.Strip(d.View()); !strings.Contains(view, "base is bad") {
			t.Fatalf("error not rendered: %q", view)
		}
	})

	t.Run("invalid first field blocks enter from second field", func(t *testing.T) {
		wantTwo := func(s string) string {
			if s != "2" {
				return "need 2"
			}
			return ""
		}
		d := NewInputDialog("id", "Title", "hint").
			SetInputValidate(wantTwo).
			SetSecondInput("Base", "hint", nil)
		d.Show()
		d, _ = pressKey(t, d, tab())
		d = typeRunes(t, d, "main")
		if _, cmd := pressKey(t, d, enter()); cmd != nil {
			t.Fatal("enter accepted while the first field was invalid")
		}
		if d.validationErr == "" {
			t.Fatal("first field error not recorded")
		}
	})
}

// TestDialogSingleFieldUnchanged proves a dialog that never opts into the
// second field keeps its single-field behavior: tab falls through to the
// textinput's own handling and results carry an empty Value2.
func TestDialogSingleFieldUnchanged(t *testing.T) {
	d := NewInputDialog("id", "Title", "hint")
	d.Show()
	d = typeRunes(t, d, "ws")

	// tab/shift-tab never consume on a single-field input dialog — the key
	// reaches the textinput (which ignores it) rather than switching focus.
	d, _ = pressKey(t, d, tab())
	if d.focused2 {
		t.Fatal("tab switched focus on a single-field dialog")
	}

	_, cmd := pressKey(t, d, enter())
	res, ok := cmd().(DialogResult)
	if !ok || !res.Confirmed || res.Value != "ws" || res.Value2 != "" {
		t.Fatalf("single-field result = %+v", res)
	}
}

// TestDialogSecondInputShowReset proves reopening clears the second field and
// returns focus to the first.
func TestDialogSecondInputShowReset(t *testing.T) {
	d := NewInputDialog("id", "Title", "hint").SetSecondInput("Base", "hint", nil)
	d.Show()
	d = typeRunes(t, d, "ws")
	d, _ = pressKey(t, d, tab())
	d = typeRunes(t, d, "main")

	d.Show()
	if d.focused2 {
		t.Fatal("reopen left focus on the second field")
	}
	if d.input2.Value() != "" || d.input.Value() != "" {
		t.Fatalf("reopen left stale input: %q %q", d.input.Value(), d.input2.Value())
	}
}
