package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDialogConfirmEnterDefaultsToNo(t *testing.T) {
	d := NewConfirmDialog("trust_scripts", "Trust Scripts?", "Run repo scripts?")
	d.SetDefaultOption(1)
	d.SetSize(80, 24)
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from pressing Enter")
	}
	result, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", result)
	}
	if result.Confirmed {
		t.Fatal("expected default Enter on confirm dialog to cancel")
	}
}

func TestDialogConfirmReopenResetsToNo(t *testing.T) {
	d := NewConfirmDialog("trust_scripts", "Trust Scripts?", "Run repo scripts?")
	d.SetDefaultOption(1)
	d.SetSize(80, 24)
	d.Show()

	d.cursor = 0
	d.visible = false
	d.Show()

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from pressing Enter")
	}
	result, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("expected DialogResult, got %T", result)
	}
	if result.Confirmed {
		t.Fatal("expected reopened confirm dialog to default to cancel")
	}
}
