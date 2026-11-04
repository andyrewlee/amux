// Add-input machinery shared by EnvDialog and SettingsDialog's Assistants
// section: the ctrl+a two-field add mode (name, then value/command).
// add_model.go holds the verified table; the dialogs keep their existing
// (adding, addField) pair as the numeric projection of these states, and
// add_model_test.go differentially asserts both dialogs walk the table.

package common

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// addState is the add-input mode's control state. addField's 0/1 sentinel
// becomes two explicit states here; the typed text, the in-dialog error
// string, and the transient footer notice are extended state at the call
// site (the error paths are self-loops — the field stays put and only the
// rendered message changes; the notice is cleared by the next keypress and
// is not a state at all).
type addState uint8

const (
	// addBrowsing: no add input open — the row list owns input.
	addBrowsing addState = iota
	// addNameEntry: the add input is open, focused on the name field.
	addNameEntry
	// addCmdEntry: the add input is open, focused on the value/command
	// field.
	addCmdEntry
)

func (s addState) String() string {
	switch s {
	case addNameEntry:
		return "adding-name"
	case addCmdEntry:
		return "adding-cmd"
	default:
		return "browsing"
	}
}

// addEvent is the add mode's alphabet.
type addEvent uint8

const (
	// evtAddOpen: ctrl+a from the list.
	evtAddOpen addEvent = iota
	// evtAddToggleField: tab/shift+tab swaps name ⇄ command.
	evtAddToggleField
	// evtAddNameAccepted: enter on the name field with a valid name.
	evtAddNameAccepted
	// evtAddNameRejected: enter with an invalid name — stay, show addError.
	evtAddNameRejected
	// evtAddCommitAccepted: enter on the command field resolved the add
	// (a committed pair, or a duplicate name which closes the input with a
	// notice and focuses the existing row).
	evtAddCommitAccepted
	// evtAddCommitRejected: the commit failed validation — stay, show
	// addError (Settings: an empty command; Env: unreachable, its name is
	// pre-validated, but the edge is legal and shared).
	evtAddCommitRejected
	// evtAddCancel: esc cancels just the add, leaving a footer notice.
	evtAddCancel
)

func (e addEvent) String() string {
	switch e {
	case evtAddOpen:
		return "open"
	case evtAddToggleField:
		return "toggle-field"
	case evtAddNameAccepted:
		return "name-accepted"
	case evtAddNameRejected:
		return "name-rejected"
	case evtAddCommitAccepted:
		return "commit-accepted"
	case evtAddCommitRejected:
		return "commit-rejected"
	default:
		return "cancel"
	}
}

// addModeMachine is the two-field add input shared by both dialogs:
// Browsing → AddingName → AddingCmd → Browsing, with validation failures
// as self-loops. The command field is reachable only through the name
// field; the add ends only by commit or cancel.
var addModeMachine = fsm.MustCompile(fsm.Machine[addState, addEvent]{
	Name:    "dialog-add-input",
	Initial: addBrowsing,
	Steps: []fsm.Step[addState, addEvent]{
		{From: addBrowsing, Event: evtAddOpen, To: addNameEntry},

		{From: addNameEntry, Event: evtAddToggleField, To: addCmdEntry},
		{From: addNameEntry, Event: evtAddNameAccepted, To: addCmdEntry},
		{From: addNameEntry, Event: evtAddNameRejected, To: addNameEntry},
		{From: addNameEntry, Event: evtAddCancel, To: addBrowsing},

		{From: addCmdEntry, Event: evtAddToggleField, To: addNameEntry},
		{From: addCmdEntry, Event: evtAddCommitAccepted, To: addBrowsing},
		{From: addCmdEntry, Event: evtAddCommitRejected, To: addCmdEntry},
		{From: addCmdEntry, Event: evtAddCancel, To: addBrowsing},
	},
})

// AddModeCheck formally verifies the add-input machine: the list is left
// only by opening the add; the add ends only by commit or cancel; the
// command field is unreachable directly from browsing (the name gates it);
// and a rejected validation never moves the field.
func AddModeCheck() error {
	return addModeMachine.Check(fsm.Checker[addState, addEvent]{
		Alphabet: []addEvent{
			evtAddOpen, evtAddToggleField,
			evtAddNameAccepted, evtAddNameRejected,
			evtAddCommitAccepted, evtAddCommitRejected,
			evtAddCancel,
		},
		AllDeclaredReachable: true,
		Edge: func(from addState, event addEvent, to addState) error {
			if from == addBrowsing && to != addBrowsing && event != evtAddOpen {
				return fmt.Errorf("the list was left via %v: only ctrl+a opens the add input", event)
			}
			if to == addBrowsing && from != addBrowsing &&
				event != evtAddCommitAccepted && event != evtAddCancel {
				return fmt.Errorf("the add ended via %v: only a resolved commit or esc closes it", event)
			}
			if from == addBrowsing && to == addCmdEntry {
				return errors.New("the command field is reachable only through the name field")
			}
			if (event == evtAddNameRejected && to != addNameEntry) ||
				(event == evtAddCommitRejected && to != addCmdEntry) {
				return fmt.Errorf("%v must keep the field (the error is a message, not a move)", event)
			}
			return nil
		},
	})
}

// addStateFor projects a dialog's (adding, addField) pair onto the machine's
// states — the bridge the differential tests use to assert the shipped
// field pairs never leave the table.
func addStateFor(adding bool, field int) addState {
	switch {
	case !adding:
		return addBrowsing
	case field == 0:
		return addNameEntry
	default:
		return addCmdEntry
	}
}
