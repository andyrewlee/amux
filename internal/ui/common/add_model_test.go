package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestAddModeMachineCheck runs the exhaustive verifier over the shared
// add-input machine — list left only by ctrl+a, add closed only by commit
// or cancel, command field gated behind the name field, rejections keep
// the field.
func TestAddModeMachineCheck(t *testing.T) {
	if err := AddModeCheck(); err != nil {
		t.Fatalf("add-mode machine failed verification: %v", err)
	}
}

// envAddTracker steps addModeMachine in lockstep with an EnvDialog driven
// through its real key path, failing when the dialog's (adding, addField)
// projection leaves the table.
type envAddTracker struct {
	t  *testing.T
	d  *EnvDialog
	st addState
}

func (tr *envAddTracker) step(evt addEvent) {
	tr.t.Helper()
	next, ok := addModeMachine.Next(tr.st, evt)
	if !ok {
		tr.t.Fatalf("table rejected %v from %v", evt, tr.st)
	}
	tr.st = next
	if got := addStateFor(tr.d.adding, tr.d.addField); got != tr.st {
		tr.t.Fatalf("EnvDialog drifted from the table: %v --%v--> %v, dialog projects %v", tr.st, evt, next, got)
	}
}

func (tr *envAddTracker) key(msg tea.KeyPressMsg) {
	tr.t.Helper()
	tr.d.Update(msg)
}

// TestEnvDialogAddWalksTable drives EnvDialog's add input through its full
// shape (open, rejected name, toggles, advance, cancel, and a committed
// add) and asserts every observed (adding, addField) pair is a state the
// checked table permits.
func TestEnvDialogAddWalksTable(t *testing.T) {
	d := NewEnvDialog(map[string]string{"AAA": "1"})
	d.Show()
	tr := &envAddTracker{t: t, d: d}

	tr.key(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tr.step(evtAddOpen)

	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // empty name
	tr.step(evtAddNameRejected)
	if d.addError == "" {
		t.Fatal("rejected name should surface addError")
	}

	tr.key(tea.KeyPressMsg{Code: tea.KeyTab})
	tr.step(evtAddToggleField)
	tr.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	tr.step(evtAddToggleField)

	typeIntoEnvDialog(d, "BBB")
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddNameAccepted)

	tr.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	tr.step(evtAddCancel)

	// A committed add (here: a duplicate name, which resolves the input
	// with a notice and focuses the existing row) closes via commit.
	tr.key(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tr.step(evtAddOpen)
	typeIntoEnvDialog(d, "AAA")
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddNameAccepted)
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddCommitAccepted)
}

// settingsAddTracker is envAddTracker for SettingsDialog's Assistants
// section (the second consumer of the same table).
type settingsAddTracker struct {
	t  *testing.T
	d  *SettingsDialog
	st addState
}

func (tr *settingsAddTracker) step(evt addEvent) {
	tr.t.Helper()
	next, ok := addModeMachine.Next(tr.st, evt)
	if !ok {
		tr.t.Fatalf("table rejected %v from %v", evt, tr.st)
	}
	tr.st = next
	if got := addStateFor(tr.d.assistantAdding, tr.d.assistantAddField); got != tr.st {
		tr.t.Fatalf("SettingsDialog drifted from the table: %v --%v--> %v, dialog projects %v", tr.st, evt, next, got)
	}
}

func (tr *settingsAddTracker) key(msg tea.KeyPressMsg) {
	tr.t.Helper()
	tr.d.Update(msg)
}

// TestSettingsDialogAddWalksTable drives the assistant add input through
// the same table, including the commit-rejected edge only this dialog can
// produce (an empty command keeps the input open with an error).
func TestSettingsDialogAddWalksTable(t *testing.T) {
	d := NewSettingsDialog(ThemeAyuDark, "", "", "")
	d.SetAssistants(nil, nil)
	d.Show()
	d.focusedItem = settingsItemAssistants
	tr := &settingsAddTracker{t: t, d: d}

	tr.key(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tr.step(evtAddOpen)

	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // empty name
	tr.step(evtAddNameRejected)

	typeIntoAssistant(d, "newbot")
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddNameAccepted)

	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // empty command
	tr.step(evtAddCommitRejected)
	if d.assistantAddError == "" {
		t.Fatal("rejected commit should surface assistantAddError")
	}

	typeIntoAssistant(d, "newbot --fast")
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddCommitAccepted)

	// Reopen and cancel from the command field.
	tr.key(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tr.step(evtAddOpen)
	typeIntoAssistant(d, "again")
	tr.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	tr.step(evtAddNameAccepted)
	tr.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	tr.step(evtAddCancel)
}
