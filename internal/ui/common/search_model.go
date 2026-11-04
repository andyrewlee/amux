// Search machinery shared by the `/`-field dialogs (OutputDialog here, and
// the diff viewer in internal/ui/diff): the control-state machine behind
// editing → accept → match cycling. search_model.go holds the machine and
// its runtime embodiment; the per-dialog match types, scanners, and
// rendering stay beside their dialogs (extended state).

package common

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// searchState is the `/`-field control state. The query text, the match
// list, and the selected index are extended state kept at the call site;
// this type models only which mode the dialog is in and whether the
// one-shot wrap marker is armed.
type searchState uint8

const (
	// searchIdle: no query — n/N are inert (the diff viewer routes them to
	// hunk navigation).
	searchIdle searchState = iota
	// searchEditing: the query field owns all input (keys and paste).
	searchEditing
	// searchMatched: accepted query with a live selection.
	searchMatched
	// searchWrapped: searchMatched plus the one-jump wrap marker — the
	// footer's `(wrapped)` note, cleared by the next jump or scroll.
	searchWrapped
	// searchNoMatch: query kept after accept with zero matches (footer
	// shows `0 matches`; n/N inert).
	searchNoMatch
	// searchCleared: query kept, matches exist, but the selected line was
	// evicted by a content refresh (selection index -1). Recoverable: a
	// jump re-selects the first (n) or last (N) match. Only the output
	// dialog's repair rule can land here — the diff viewer's repair falls
	// back to the last surviving match instead (pinned drift, below).
	searchCleared
)

func (s searchState) String() string {
	switch s {
	case searchEditing:
		return "editing"
	case searchMatched:
		return "matched"
	case searchWrapped:
		return "wrapped"
	case searchNoMatch:
		return "no-match"
	case searchCleared:
		return "cleared"
	default:
		return "idle"
	}
}

// SearchEvent is the search machine's alphabet. The close/refresh events
// encode their outcome (what the field or refresh left behind); the call
// site reduces its extended state (query, match count, selection) to the
// right event before applying it.
type SearchEvent uint8

const (
	// SearchOpenField: `/` opens (or reopens) the query field from any
	// non-editing state — from an accepted state, any edit returns here
	// with the query retained.
	SearchOpenField SearchEvent = iota
	// SearchCloseIdle: the field closed with an empty query — search state
	// dropped entirely.
	SearchCloseIdle
	// SearchCloseMatched: the field closed with matches (accept selects the
	// first and jumps; esc retains the repaired selection).
	SearchCloseMatched
	// SearchCloseNoMatch: the field closed with a kept query and zero
	// matches.
	SearchCloseNoMatch
	// SearchCloseCleared: esc closed the field while the selection was
	// evicted (matches exist, index -1). Accept can never produce this.
	SearchCloseCleared
	// SearchJumpStay: n/N without wrapping — clears the wrap marker.
	SearchJumpStay
	// SearchJumpWrap: n/N wrapping at either end — (re)arms the marker.
	SearchJumpWrap
	// SearchJumpRecover: n/N from cleared — re-selects the first/last match
	// and never arms the marker.
	SearchJumpRecover
	// SearchScroll: any manual scroll or the follow toggle — clears the
	// wrap marker, leaves everything else.
	SearchScroll
	// SearchRefreshKept: a content refresh recomputed matches without a
	// qualitative change.
	SearchRefreshKept
	// SearchRefreshFound: a refresh grew matches where none were.
	SearchRefreshFound
	// SearchRefreshRecovered: a refresh restored a selection from cleared.
	SearchRefreshRecovered
	// SearchRefreshEvicted: a refresh dropped the selected line with no
	// survivor at-or-after it.
	SearchRefreshEvicted
	// SearchRefreshEmptied: a refresh left zero matches.
	SearchRefreshEmptied
	// SearchDrop: the diff viewer's browse-mode esc (clear-marks idiom) and
	// reload — the whole search state is dropped from any state.
	SearchDrop
)

func (e SearchEvent) String() string {
	switch e {
	case SearchOpenField:
		return "open-field"
	case SearchCloseIdle:
		return "close-idle"
	case SearchCloseMatched:
		return "close-matched"
	case SearchCloseNoMatch:
		return "close-no-match"
	case SearchCloseCleared:
		return "close-cleared"
	case SearchJumpStay:
		return "jump-stay"
	case SearchJumpWrap:
		return "jump-wrap"
	case SearchJumpRecover:
		return "jump-recover"
	case SearchScroll:
		return "scroll"
	case SearchRefreshKept:
		return "refresh-kept"
	case SearchRefreshFound:
		return "refresh-found"
	case SearchRefreshRecovered:
		return "refresh-recovered"
	case SearchRefreshEvicted:
		return "refresh-evicted"
	case SearchRefreshEmptied:
		return "refresh-emptied"
	default:
		return "drop"
	}
}

// searchMachine is the one `/`-field transition table, shared by
// OutputDialog and the diff viewer. The two had drifted (the survey flagged
// paste-newline-accepts, but both dialogs implement it); the drift that
// remains is deliberate and pinned here:
//
//   - Cleared→matched recovery (n re-selects the first match, N the last)
//     exists only in the output dialog; the diff viewer's repair rule falls
//     back to the LAST surviving match, so it never enters searchCleared at
//     all and its n falls through to hunk navigation when nothing is
//     selected.
//   - follow mode (`f`, disengage-on-manual-scroll) is output-dialog-only
//     and orthogonal to search; it stays a bool at that call site.
//   - the diff viewer additionally uses SearchDrop (esc-with-query clears
//     the search; a reload invalidates every span) where the output
//     dialog's esc closes the whole dialog.
//
// Editing self-loops on the refresh events because live recompute while
// typing never leaves the field.
var searchMachine = fsm.MustCompile(fsm.Machine[searchState, SearchEvent]{
	Name:    "dialog-search",
	Initial: searchIdle,
	Steps: []fsm.Step[searchState, SearchEvent]{
		{From: searchIdle, Event: SearchOpenField, To: searchEditing},
		{From: searchIdle, Event: SearchScroll, To: searchIdle},

		{From: searchEditing, Event: SearchCloseIdle, To: searchIdle},
		{From: searchEditing, Event: SearchCloseMatched, To: searchMatched},
		{From: searchEditing, Event: SearchCloseNoMatch, To: searchNoMatch},
		{From: searchEditing, Event: SearchCloseCleared, To: searchCleared},
		{From: searchEditing, Event: SearchRefreshKept, To: searchEditing},
		{From: searchEditing, Event: SearchRefreshFound, To: searchEditing},
		{From: searchEditing, Event: SearchRefreshRecovered, To: searchEditing},
		{From: searchEditing, Event: SearchRefreshEvicted, To: searchEditing},
		{From: searchEditing, Event: SearchRefreshEmptied, To: searchEditing},
		{From: searchEditing, Event: SearchDrop, To: searchIdle},

		{From: searchMatched, Event: SearchOpenField, To: searchEditing},
		{From: searchMatched, Event: SearchJumpStay, To: searchMatched},
		{From: searchMatched, Event: SearchJumpWrap, To: searchWrapped},
		{From: searchMatched, Event: SearchScroll, To: searchMatched},
		{From: searchMatched, Event: SearchRefreshKept, To: searchMatched},
		{From: searchMatched, Event: SearchRefreshEvicted, To: searchCleared},
		{From: searchMatched, Event: SearchRefreshEmptied, To: searchNoMatch},
		{From: searchMatched, Event: SearchDrop, To: searchIdle},

		{From: searchWrapped, Event: SearchOpenField, To: searchEditing},
		{From: searchWrapped, Event: SearchJumpStay, To: searchMatched},
		{From: searchWrapped, Event: SearchJumpWrap, To: searchWrapped},
		{From: searchWrapped, Event: SearchScroll, To: searchMatched},
		{From: searchWrapped, Event: SearchRefreshKept, To: searchWrapped},
		{From: searchWrapped, Event: SearchRefreshEvicted, To: searchCleared},
		{From: searchWrapped, Event: SearchRefreshEmptied, To: searchNoMatch},
		{From: searchWrapped, Event: SearchDrop, To: searchIdle},

		{From: searchNoMatch, Event: SearchOpenField, To: searchEditing},
		{From: searchNoMatch, Event: SearchJumpStay, To: searchNoMatch}, // n/N inert with no matches
		{From: searchNoMatch, Event: SearchScroll, To: searchNoMatch},
		{From: searchNoMatch, Event: SearchRefreshKept, To: searchNoMatch},
		{From: searchNoMatch, Event: SearchRefreshFound, To: searchMatched},
		{From: searchNoMatch, Event: SearchRefreshEmptied, To: searchNoMatch},
		{From: searchNoMatch, Event: SearchDrop, To: searchIdle},

		{From: searchCleared, Event: SearchOpenField, To: searchEditing},
		{From: searchCleared, Event: SearchJumpRecover, To: searchMatched},
		{From: searchCleared, Event: SearchScroll, To: searchCleared},
		{From: searchCleared, Event: SearchRefreshKept, To: searchCleared},
		{From: searchCleared, Event: SearchRefreshRecovered, To: searchMatched},
		{From: searchCleared, Event: SearchRefreshEmptied, To: searchNoMatch},
		{From: searchCleared, Event: SearchDrop, To: searchIdle},
	},
})

// SearchCheck formally verifies the shared search machine: the wrap marker
// is armed only by a wrapping jump and disarmed by the next non-wrapping
// jump or scroll (one-shot); any edit from an accepted state reopens the
// field; a no-match close never carries a selection; recovery from cleared
// re-selects without arming the marker; idle is left only by opening the
// field; and a drop always returns to idle.
func SearchCheck() error {
	return searchMachine.Check(fsm.Checker[searchState, SearchEvent]{
		Alphabet: []SearchEvent{
			SearchOpenField,
			SearchCloseIdle, SearchCloseMatched, SearchCloseNoMatch, SearchCloseCleared,
			SearchJumpStay, SearchJumpWrap, SearchJumpRecover, SearchScroll,
			SearchRefreshKept, SearchRefreshFound, SearchRefreshRecovered,
			SearchRefreshEvicted, SearchRefreshEmptied,
			SearchDrop,
		},
		AllDeclaredReachable: true,
		Edge: func(from searchState, event SearchEvent, to searchState) error {
			if to == searchWrapped && from != searchWrapped && event != SearchJumpWrap {
				return fmt.Errorf("wrap marker armed via %v: only a wrapping jump may arm it", event)
			}
			if from == searchWrapped && event == SearchJumpStay && to != searchMatched {
				return errors.New("a non-wrapping jump must clear the wrap marker (one-shot)")
			}
			if event == SearchOpenField && to != searchEditing {
				return errors.New("opening the field must always enter editing (any edit returns to the field)")
			}
			if event == SearchCloseIdle && to != searchIdle {
				return errors.New("closing with an empty query must drop the search state")
			}
			if event == SearchCloseNoMatch && to != searchNoMatch {
				return errors.New("closing with zero matches must never select a row")
			}
			if event == SearchCloseCleared && to != searchCleared {
				return errors.New("esc with an evicted selection stays cleared, not re-selected")
			}
			if event == SearchJumpRecover && to != searchMatched {
				return errors.New("recovery from cleared must re-select without arming the wrap marker")
			}
			if event == SearchDrop && to != searchIdle {
				return errors.New("a drop must return to idle")
			}
			if from == searchIdle && to != searchIdle && event != SearchOpenField {
				return fmt.Errorf("idle left via %v: only `/` opens the field", event)
			}
			return nil
		},
	})
}

// SearchCtl is the runtime embodiment of searchMachine — the per-dialog
// control state. The call site owns the query, match list, and index, and
// reduces them to SearchEvents. Rejected steps (an event the table does not
// declare for the current state) are no-ops, matching the dialogs' inert
// keys.
type SearchCtl struct {
	st searchState
}

// Apply steps the machine; a transition the table rejects is a no-op.
func (c *SearchCtl) Apply(e SearchEvent) {
	if next, ok := searchMachine.Next(c.st, e); ok {
		c.st = next
	}
}

// Editing reports whether the query field owns all input.
func (c SearchCtl) Editing() bool { return c.st == searchEditing }

// WrapMarked reports whether the one-shot `(wrapped)` footer marker is
// armed for the current selection.
func (c SearchCtl) WrapMarked() bool { return c.st == searchWrapped }

// LeaveEvent reduces "the field closed without accepting" (esc) to its
// outcome event: the query is retained, the repaired selection stays, and
// only an empty query drops the search state.
func (c SearchCtl) LeaveEvent(query string, matches, matchIdx int) SearchEvent {
	switch {
	case query == "":
		return SearchCloseIdle
	case matches == 0:
		return SearchCloseNoMatch
	case matchIdx >= 0:
		return SearchCloseMatched
	default:
		return SearchCloseCleared
	}
}

// RefreshEvent reduces a recomputed match list to its refresh event:
// matches is the new count, selected whether an index survives. Selected
// transitions out of no-match are finds; a lost selection from an accepted
// state is an eviction (or a recovery, when cleared already).
func (c SearchCtl) RefreshEvent(matches int, selected bool) SearchEvent {
	if matches == 0 {
		return SearchRefreshEmptied
	}
	switch {
	case selected:
		if c.st == searchNoMatch {
			return SearchRefreshFound
		}
		if c.st == searchCleared {
			// A selection recovered out of the cleared state — without
			// this edge the ctl strands in cleared while the dialog holds
			// a live selection, and every later jump is rejected no-op.
			return SearchRefreshRecovered
		}
		return SearchRefreshKept
	case c.st == searchMatched || c.st == searchWrapped:
		return SearchRefreshEvicted
	case c.st == searchCleared:
		return SearchRefreshKept // still cleared — nothing new to recover
	default:
		// Editing (live recompute) and idle are self-loops on every refresh
		// event; kept is as good a representative as any.
		return SearchRefreshKept
	}
}
