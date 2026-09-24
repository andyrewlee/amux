package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestScriptsDialog_EditRunCommandAndConfirm(t *testing.T) {
	d := NewScriptsDialog("", "npm run dev", "", "", "nonconcurrent")
	d.Show()

	// Cursor starts on setup; move to run (row 1), append text, confirm.
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a result cmd on Enter")
	}
	res, ok := cmd().(ScriptsDialogResult)
	if !ok || res.Canceled {
		t.Fatalf("expected non-canceled ScriptsDialogResult, got %#v", cmd())
	}

	setup, run, archive, onDone, mode := d.Values()
	if setup != "" || run != "npm run devX" || archive != "" || onDone != "" || mode != "nonconcurrent" {
		t.Fatalf("Values() = (%q,%q,%q,%q,%q)", setup, run, archive, onDone, mode)
	}
}

func TestScriptsDialog_ModeToggle(t *testing.T) {
	d := NewScriptsDialog("", "", "", "", "concurrent")
	d.Show()

	// Move to the mode row (4 downs: setup → run → archive → on-done → mode),
	// toggle with space.
	for i := 0; i < 4; i++ {
		d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	d.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	_, _, _, _, mode := d.Values()
	if mode != "nonconcurrent" {
		t.Fatalf("mode after toggle = %q, want nonconcurrent", mode)
	}
	// Space on the mode row must not leak into a text field.
	setup, run, archive, _, _ := d.Values()
	if setup+run+archive != "" {
		t.Fatalf("space leaked into a command field: %q %q %q", setup, run, archive)
	}
}

func TestScriptsDialog_EscCancelsDiscardingEdits(t *testing.T) {
	d := NewScriptsDialog("", "make build", "", "", "nonconcurrent")
	d.Show()
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	d.Update(tea.KeyPressMsg{Code: 'Y', Text: "Y"})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	res, ok := cmd().(ScriptsDialogResult)
	if !ok || !res.Canceled {
		t.Fatalf("expected canceled result, got %#v", cmd())
	}
	if d.Visible() {
		t.Fatal("dialog should hide on Esc")
	}
}

func TestScriptsDialog_UnknownModeDefaultsToNonconcurrent(t *testing.T) {
	d := NewScriptsDialog("", "", "", "", "weird")
	if _, _, _, _, mode := d.Values(); mode != "nonconcurrent" {
		t.Fatalf("mode = %q, want nonconcurrent for unknown input", mode)
	}
}

func TestScriptsDialog_ViewShowsTrustCaveat(t *testing.T) {
	d := NewScriptsDialog("", "", "", "", "nonconcurrent")
	d.Show()
	d.SetSize(100, 40)
	view := d.View()
	if !strings.Contains(view, "without a trust prompt") {
		t.Fatalf("view must render the trust caveat, got:\n%s", view)
	}
	if !strings.Contains(view, "run:") || !strings.Contains(view, "mode:") {
		t.Fatalf("view must render all fields, got:\n%s", view)
	}
}
