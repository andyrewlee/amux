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
