package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestDialogValidationOnEnter proves Enter validates the CURRENT input value
// before a DialogInput accepts: the cached validationErr only reflects
// keystrokes, so an empty or programmatically prefilled value must still be
// re-checked at submission.
func TestDialogValidationOnEnter(t *testing.T) {
	wantTwo := func(s string) string {
		if s != "2" {
			return "Type 2 to confirm"
		}
		return ""
	}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}

	pressEnter := func(t *testing.T, d *Dialog) (result *DialogResult, stillOpen bool) {
		t.Helper()
		d, cmd := d.Update(enter)
		stillOpen = d.Visible()
		if cmd != nil {
			if r, ok := cmd().(DialogResult); ok {
				result = &r
			}
		}
		return result, stillOpen
	}

	t.Run("immediate enter on empty is blocked", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint").SetInputValidate(wantTwo)
		d.Show()
		if res, open := pressEnter(t, d); res != nil || !open {
			t.Fatalf("empty enter accepted: res=%+v open=%v", res, open)
		}
	})

	t.Run("validation error renders after blocked enter", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint").SetInputValidate(wantTwo)
		d.Show()
		pressEnter(t, d)
		if d.validationErr == "" {
			t.Fatal("blocked enter left validationErr empty")
		}
	})

	t.Run("invalid prefilled value is blocked", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint").SetInputValidate(wantTwo)
		d.Show()
		d.SetInputValue("3")
		if res, open := pressEnter(t, d); res != nil || !open {
			t.Fatalf("invalid prefilled enter accepted: res=%+v open=%v", res, open)
		}
	})

	t.Run("valid prefilled value confirms", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint").SetInputValidate(wantTwo)
		d.Show()
		d.SetInputValue("2")
		res, open := pressEnter(t, d)
		if res == nil || !res.Confirmed || res.Value != "2" {
			t.Fatalf("valid prefilled enter rejected: res=%+v", res)
		}
		if open {
			t.Fatal("confirmed dialog still visible")
		}
	})

	t.Run("no validator accepts immediately", func(t *testing.T) {
		d := NewInputDialog("id", "Title", "hint")
		d.Show()
		res, open := pressEnter(t, d)
		if res == nil || !res.Confirmed {
			t.Fatalf("no-validator enter produced no result: res=%+v", res)
		}
		if open {
			t.Fatal("no-validator dialog still visible")
		}
	})
}
