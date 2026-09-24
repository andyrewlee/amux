package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// typeIntoEnvDialog drives d's real key router (Update) rather than poking
// fields directly, mirroring settings_assistants_test.go's typeInto helper.
func typeIntoEnvDialog(d *EnvDialog, s string) {
	for _, r := range s {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestNewEnvDialogSeedsSortedCopy(t *testing.T) {
	env := map[string]string{"NODE_ENV": "production", "API_KEY": "secret"}
	d := NewEnvDialog(env)

	// Mutating the caller's map after construction must not affect the
	// dialog's seeded copy.
	env["API_KEY"] = "mutated"

	got := d.Env()
	if got["API_KEY"] != "secret" {
		t.Fatalf("API_KEY = %q, want %q (dialog must copy env, not alias it)", got["API_KEY"], "secret")
	}
	if got["NODE_ENV"] != "production" {
		t.Fatalf("NODE_ENV = %q, want %q", got["NODE_ENV"], "production")
	}
	if len(d.keys) != 2 || d.keys[0] != "API_KEY" || d.keys[1] != "NODE_ENV" {
		t.Fatalf("keys = %#v, want sorted [API_KEY NODE_ENV]", d.keys)
	}
}

func TestEnvDialogEnvReturnsCopyNotAlias(t *testing.T) {
	d := NewEnvDialog(map[string]string{"FOO": "bar"})
	got := d.Env()
	got["FOO"] = "mutated-by-caller"

	if d.values["FOO"] != "bar" {
		t.Fatalf("Env() mutation leaked into dialog state: values[FOO] = %q, want %q", d.values["FOO"], "bar")
	}
}

func TestEnvDialogEditsFocusedValue(t *testing.T) {
	d := NewEnvDialog(map[string]string{"API_KEY": "", "NODE_ENV": "dev"})
	d.Show()

	// Cursor starts at row 0 (API_KEY, sorted first).
	typeIntoEnvDialog(d, "sk-123")
	if got := d.Env()["API_KEY"]; got != "sk-123" {
		t.Fatalf("API_KEY = %q, want %q", got, "sk-123")
	}

	// Down moves to NODE_ENV; typing (including letters like j/k) edits its
	// value, not list navigation -- mirrors the assistant-field contract.
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	typeIntoEnvDialog(d, "jk-prod")
	if got := d.Env()["NODE_ENV"]; got != "devjk-prod" {
		t.Fatalf("NODE_ENV = %q, want %q", got, "devjk-prod")
	}
	// The other row's value must be untouched.
	if got := d.Env()["API_KEY"]; got != "sk-123" {
		t.Fatalf("API_KEY changed unexpectedly: %q", got)
	}

	// Backspace trims the last rune of the focused (NODE_ENV) value.
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := d.Env()["NODE_ENV"]; got != "devjk-pro" {
		t.Fatalf("NODE_ENV after backspace = %q, want %q", got, "devjk-pro")
	}
}

func TestEnvDialogCursorWraps(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1", "B": "2"})
	d.Show()

	if d.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", d.cursor)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.cursor != 1 {
		t.Fatalf("cursor after Down = %d, want 1", d.cursor)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if d.cursor != 0 {
		t.Fatalf("cursor after wrapping Down = %d, want 0", d.cursor)
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if d.cursor != 1 {
		t.Fatalf("cursor after wrapping Up = %d, want 1", d.cursor)
	}
}

func TestEnvDialogCtrlDRemovesFocusedPairOnly(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1", "B": "2", "C": "3"})
	d.Show()
	// Cursor at row 0 = "A".

	d.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})

	got := d.Env()
	if _, ok := got["A"]; ok {
		t.Fatalf("expected A removed, got %#v", got)
	}
	if got["B"] != "2" || got["C"] != "3" {
		t.Fatalf("expected B and C untouched, got %#v", got)
	}
	if len(d.keys) != 2 {
		t.Fatalf("keys = %#v, want 2 remaining rows", d.keys)
	}
}

func TestEnvDialogRemoveLastRowClampsCursor(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	d.Show()

	d.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})

	if len(d.Env()) != 0 {
		t.Fatalf("expected empty map after removing the only pair, got %#v", d.Env())
	}
	if d.cursor != 0 {
		t.Fatalf("cursor after removing last row = %d, want 0", d.cursor)
	}

	// Further navigation/removal/typing on an empty roster must not panic.
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	typeIntoEnvDialog(d, "x")
	if len(d.Env()) != 0 {
		t.Fatalf("expected still-empty map, got %#v", d.Env())
	}
}

func TestEnvDialogRemoveThenEditNextRow(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1", "B": "2"})
	d.Show()

	// Remove "A" (row 0); cursor clamps to the new row 0, which is now "B".
	d.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	typeIntoEnvDialog(d, "x")

	got := d.Env()
	if got["B"] != "2x" {
		t.Fatalf("B = %q, want %q (edit should land on the row now under the cursor)", got["B"], "2x")
	}
}

func TestEnvDialogEscCancelsWithoutMutating(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	d.Show()
	typeIntoEnvDialog(d, "edited")

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.Visible() {
		t.Fatal("esc should hide the dialog")
	}
	result, ok := cmd().(EnvDialogResult)
	if !ok || !result.Canceled {
		t.Fatalf("expected canceled EnvDialogResult, got %#v (ok=%v)", cmd(), ok)
	}
	// Esc reports canceled; the CALLER (internal/app) is responsible for
	// discarding a.envDialog rather than reading Env() back on this path. The
	// in-memory edit is still visible here (the widget itself does not revert
	// it) -- asserting that documents the contract the app-layer test in
	// internal/app pins: a canceled result must never reach the persist path.
	if got := d.Env()["A"]; got != "1edited" {
		t.Fatalf("Env() after cancel = %q, want the in-memory edit %q (caller must discard, not the widget)", got, "1edited")
	}
}

func TestEnvDialogEnterConfirms(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	d.Show()
	typeIntoEnvDialog(d, "x")

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.Visible() {
		t.Fatal("enter should hide the dialog")
	}
	result, ok := cmd().(EnvDialogResult)
	if !ok || result.Canceled {
		t.Fatalf("expected a confirmed EnvDialogResult, got %#v (ok=%v)", cmd(), ok)
	}
	if got := d.Env()["A"]; got != "1x" {
		t.Fatalf("A = %q, want %q", got, "1x")
	}
}

func TestEnvDialogEmptyRosterIsNoop(t *testing.T) {
	d := NewEnvDialog(nil)
	d.Show()

	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	d.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	d.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})

	if len(d.Env()) != 0 {
		t.Fatalf("expected no env vars for an empty roster, got %#v", d.Env())
	}
}

func TestEnvDialogUpdateIgnoredWhenNotVisible(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	// Note: Show() is never called.

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("expected nil cmd when the dialog is not visible")
	}
	if got := d.Env()["A"]; got != "1" {
		t.Fatalf("A = %q, want unchanged %q", got, "1")
	}
}

func TestEnvDialogRenderShowsRowsAndHighlightsCursor(t *testing.T) {
	d := NewEnvDialog(map[string]string{"API_KEY": "secret", "NODE_ENV": "dev"})
	d.Show()
	d.cursor = 1 // NODE_ENV

	view := d.View()
	if !strings.Contains(view, "Workspace Environment") {
		t.Fatalf("expected a title, got:\n%s", view)
	}
	if !strings.Contains(view, "API_KEY: secret") {
		t.Fatalf("expected API_KEY row, got:\n%s", view)
	}
	if !strings.Contains(view, "NODE_ENV: dev") {
		t.Fatalf("expected NODE_ENV row, got:\n%s", view)
	}
}

func TestEnvDialogViewEmptyWhenNotVisible(t *testing.T) {
	d := NewEnvDialog(map[string]string{"A": "1"})
	if got := d.View(); got != "" {
		t.Fatalf("View() on a hidden dialog = %q, want empty", got)
	}
}

func TestEnvDialogViewNoRowsShowsPlaceholder(t *testing.T) {
	d := NewEnvDialog(nil)
	d.Show()
	view := d.View()
	if !strings.Contains(view, "No editable environment variables.") {
		t.Fatalf("expected empty-roster placeholder, got:\n%s", view)
	}
}

// TestEnvDialogAddFirstEntry covers the unreachable-today case: an empty map
// gets its first pair entirely through the ctrl+a add flow.
func TestEnvDialogAddFirstEntry(t *testing.T) {
	d := NewEnvDialog(nil)
	d.Show()

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !d.adding {
		t.Fatal("ctrl+a did not enter add mode")
	}
	typeIntoEnvDialog(d, "API_URL")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.addField != 1 {
		t.Fatalf("enter on a valid name did not advance to the value field (addField=%d)", d.addField)
	}
	typeIntoEnvDialog(d, "http://x")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.adding {
		t.Fatal("commit did not exit add mode")
	}
	if got := d.Env()["API_URL"]; got != "http://x" {
		t.Fatalf("Env()[API_URL] = %q, want %q", got, "http://x")
	}
	if len(d.keys) != 1 || d.keys[0] != "API_URL" || d.cursor != 0 {
		t.Fatalf("keys=%#v cursor=%d — new row must be focused", d.keys, d.cursor)
	}
}

// TestEnvDialogAddSortedInsertAndDuplicate pins the add ordering (new key
// lands at its sorted position, existing rows don't reshuffle) and the
// duplicate contract (add cancels, cursor focuses the existing row, value
// untouched).
func TestEnvDialogAddSortedInsertAndDuplicate(t *testing.T) {
	d := NewEnvDialog(map[string]string{"AAA": "1", "CCC": "3", "ZZZ": "9"})
	d.Show()

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoEnvDialog(d, "BBB")
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeIntoEnvDialog(d, "2")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	want := []string{"AAA", "BBB", "CCC", "ZZZ"}
	if len(d.keys) != len(want) {
		t.Fatalf("keys = %#v, want %#v", d.keys, want)
	}
	for i, k := range want {
		if d.keys[i] != k {
			t.Fatalf("keys[%d] = %q, want %q (full list %#v)", i, d.keys[i], k, d.keys)
		}
	}
	if d.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (the inserted BBB row)", d.cursor)
	}

	// Duplicate: add the same name again — must not overwrite, must focus
	// the existing row.
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoEnvDialog(d, "CCC")
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeIntoEnvDialog(d, "OVERWRITE")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if d.adding {
		t.Fatal("duplicate add should cancel add mode")
	}
	if d.values["CCC"] != "3" {
		t.Fatalf("duplicate add overwrote the existing value: CCC = %q, want %q", d.values["CCC"], "3")
	}
	if d.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the existing CCC row)", d.cursor)
	}
	if d.notice == "" {
		t.Fatal("duplicate add should leave a notice explaining the outcome")
	}
}

// TestEnvDialogAddValidation covers the name rules and the SetKeyValidator
// domain hook (reserved-name rejection).
func TestEnvDialogAddValidation(t *testing.T) {
	d := NewEnvDialog(nil)
	d.SetKeyValidator(func(name string) string {
		if name == "AMUX_SESSION" {
			return name + " is reserved"
		}
		return ""
	})
	d.Show()

	// Empty name: Enter on the name field stays put with an error.
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.addField != 0 || d.addError == "" {
		t.Fatalf("empty name should stay on the name field with an error (field=%d err=%q)", d.addField, d.addError)
	}

	// Invalid characters ('=' inside the name) are rejected.
	d.addName = "BAD=NAME"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.addField != 0 || d.addError == "" {
		t.Fatalf("'=' in name should stay on the name field with an error (field=%d err=%q)", d.addField, d.addError)
	}

	// Reserved name rejected via the wired validator.
	d.addName = "AMUX_SESSION"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.addField != 0 || !strings.Contains(d.addError, "reserved") {
		t.Fatalf("reserved name should be rejected via SetKeyValidator (field=%d err=%q)", d.addField, d.addError)
	}

	// Valid name advances.
	d.addName = "GOOD_NAME"
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.addField != 1 || d.addError != "" {
		t.Fatalf("valid name should advance to the value field (field=%d err=%q)", d.addField, d.addError)
	}
}

// TestEnvDialogAddCancel covers the Esc-cancels-just-the-add contract: list
// edits survive, no pair is created, and the dialog stays open.
func TestEnvDialogAddCancel(t *testing.T) {
	d := NewEnvDialog(map[string]string{"KEEP": "1"})
	d.Show()

	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	typeIntoEnvDialog(d, "PARTIAL")
	d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if d.adding {
		t.Fatal("esc did not cancel add mode")
	}
	if !d.visible {
		t.Fatal("esc inside add mode must not close the dialog")
	}
	if _, ok := d.values["PARTIAL"]; ok {
		t.Fatal("canceled add must not create a pair")
	}
	if len(d.Env()) != 1 {
		t.Fatalf("Env() = %#v — existing pairs must survive an add cancel", d.Env())
	}

	// A real Esc on the list still cancels the whole dialog.
	d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if d.visible {
		t.Fatal("esc on the list should close the dialog")
	}
}
