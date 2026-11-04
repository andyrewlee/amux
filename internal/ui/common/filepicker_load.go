// FilePicker's directory-load lifecycle: the checked table behind the
// needsLoad/loadInFlight flag pair. filepicker_load.go holds the machine;
// the picker keeps the two flags as the runtime projection of these states
// (the mapping is a bijection — see pickerStateFor), and
// filepicker_load_test.go asserts the shipped flags walk the table.

package common

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// pickerLoadState is the load lifecycle's control state. The state is a
// bijection with the picker's (needsLoad, loadInFlight) flag pair — see
// pickerStateFor — which is what lets the flags be the runtime and this
// table be the spec without a second source of truth.
type pickerLoadState uint8

const (
	// pickerClean: the listing matches currentPath — nothing due, nothing
	// in flight. (Both flags clear; reached only via a fresh result.)
	pickerClean pickerLoadState = iota
	// pickerDirty: a read is due (navigation, show, hidden-toggle) and not
	// yet issued.
	pickerDirty
	// pickerInFlight: a ReadDir is out (loadInFlight set). needsLoad stays
	// set across the issue — it is cleared only by a result for
	// currentPath — so a navigation while in flight is a self-loop here:
	// the same two flags, with the in-flight read now targeting a
	// superseded path. Which of the two is decided at result time by the
	// path comparison (fresh vs stale), not by the flags.
	pickerInFlight
)

func (s pickerLoadState) String() string {
	switch s {
	case pickerDirty:
		return "dirty"
	case pickerInFlight:
		return "in-flight"
	default:
		return "clean"
	}
}

// pickerLoadEvent is the load lifecycle's alphabet.
type pickerLoadEvent uint8

const (
	// evtPickerMarkLoad: markNeedsLoad — a navigation invalidated the
	// listing.
	evtPickerMarkLoad pickerLoadEvent = iota
	// evtPickerIssueLoad: dueLoadCmd issued the pending read (only from
	// dirty: visible ∧ needsLoad ∧ ¬loadInFlight).
	evtPickerIssueLoad
	// evtPickerFreshLoad: a directoryLoadedMsg whose path is currentPath —
	// the one edge that completes a load.
	evtPickerFreshLoad
	// evtPickerStaleLoad: a result for a superseded path — dropped, and the
	// still-set needsLoad keeps a fresh read due.
	evtPickerStaleLoad
	// evtPickerShowReset: Show clears loadInFlight (a pre-Hide result may
	// never arrive) and re-arms a reload.
	evtPickerShowReset
)

func (e pickerLoadEvent) String() string {
	switch e {
	case evtPickerMarkLoad:
		return "mark-load"
	case evtPickerIssueLoad:
		return "issue-load"
	case evtPickerFreshLoad:
		return "fresh-load"
	case evtPickerStaleLoad:
		return "stale-load"
	default:
		return "show-reset"
	}
}

// pickerLoadMachine is the load lifecycle: Clean → Dirty → InFlight →
// Clean, with the stale lane routed back to Dirty when a superseded
// result lands. The stale-result re-dirty rule was previously only implied
// by a comment in Update's directoryLoadedMsg case; here it is a table
// edge the checker enforces.
var pickerLoadMachine = fsm.MustCompile(fsm.Machine[pickerLoadState, pickerLoadEvent]{
	Name:    "filepicker-load",
	Initial: pickerClean,
	Steps: []fsm.Step[pickerLoadState, pickerLoadEvent]{
		{From: pickerClean, Event: evtPickerMarkLoad, To: pickerDirty},
		{From: pickerClean, Event: evtPickerShowReset, To: pickerDirty},

		{From: pickerDirty, Event: evtPickerMarkLoad, To: pickerDirty},
		{From: pickerDirty, Event: evtPickerIssueLoad, To: pickerInFlight},
		{From: pickerDirty, Event: evtPickerShowReset, To: pickerDirty},

		// Navigating while in flight re-arms the debt (needsLoad stays set)
		// without a second read — the flags cannot tell the two apart; the
		// result's path comparison does.
		{From: pickerInFlight, Event: evtPickerMarkLoad, To: pickerInFlight},
		// A result for currentPath completes the load even when a newer
		// mark stacked behind it (navigate away and back before it landed):
		// both flags clear together, so the projection lands on clean.
		{From: pickerInFlight, Event: evtPickerFreshLoad, To: pickerClean},
		{From: pickerInFlight, Event: evtPickerStaleLoad, To: pickerDirty},
		{From: pickerInFlight, Event: evtPickerShowReset, To: pickerDirty},
	},
})

// PickerLoadCheck formally verifies the load lifecycle: a read is issued
// only from dirty; only a result for the current path completes a load; a
// stale result never yields clean (it always re-marks the reload); and Show
// always re-arms a reload.
func PickerLoadCheck() error {
	return pickerLoadMachine.Check(fsm.Checker[pickerLoadState, pickerLoadEvent]{
		Alphabet: []pickerLoadEvent{
			evtPickerMarkLoad, evtPickerIssueLoad,
			evtPickerFreshLoad, evtPickerStaleLoad, evtPickerShowReset,
		},
		AllDeclaredReachable: true,
		Edge: func(from pickerLoadState, event pickerLoadEvent, to pickerLoadState) error {
			if event == evtPickerIssueLoad && from != pickerDirty {
				return fmt.Errorf("a read was issued from %v: only a due, not-in-flight load may issue", from)
			}
			if to == pickerClean && event != evtPickerFreshLoad {
				return fmt.Errorf("clean reached via %v: only a result for currentPath completes a load", event)
			}
			if event == evtPickerStaleLoad && to != pickerDirty {
				return errors.New("a stale result must re-mark the reload — it can never yield clean")
			}
			if event == evtPickerShowReset && to != pickerDirty {
				return errors.New("Show must always re-arm a reload")
			}
			return nil
		},
	})
}

// pickerStateFor projects the picker's (needsLoad, loadInFlight) flag pair
// onto the machine's states — a bijection (needsLoad with the read issued
// is in-flight; the never-occurring (false, true) pair folds into
// in-flight too, defensive only).
func pickerStateFor(needsLoad, loadInFlight bool) pickerLoadState {
	switch {
	case loadInFlight:
		return pickerInFlight
	case needsLoad:
		return pickerDirty
	default:
		return pickerClean
	}
}
