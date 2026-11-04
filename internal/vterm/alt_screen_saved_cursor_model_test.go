package vterm

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// savedCursorState is the alt-screen saved-cursor stash protocol's control
// state. cursorLive: no stash exists; the DECSC slot on screen belongs to
// whichever buffer is current. cursorStashed: primarySavedCursor holds the
// primary buffer's DECSC slot and the alt buffer owns a fresh one.
type savedCursorState int

const (
	cursorLive savedCursorState = iota
	cursorStashed
)

func (s savedCursorState) String() string {
	if s == cursorStashed {
		return "stashed"
	}
	return "live"
}

// savedCursorEvent is the stash protocol's alphabet. DECSC/DECRC are events
// because applications interleave them with screen switches; they write the
// *current* buffer's slot and never touch the stash.
type savedCursorEvent int

const (
	evtEnterAlt1049 savedCursorEvent = iota
	evtEnterAlt47
	evtExitAlt1049
	evtExitAlt47
	evtReattachReset
	evtDecsc
	evtDecrc
)

func (e savedCursorEvent) String() string {
	switch e {
	case evtEnterAlt1049:
		return "enter-alt-1049"
	case evtEnterAlt47:
		return "enter-alt-47"
	case evtExitAlt1049:
		return "exit-alt-1049"
	case evtExitAlt47:
		return "exit-alt-47"
	case evtReattachReset:
		return "reattach-reset"
	case evtDecsc:
		return "decsc"
	default:
		return "decrc"
	}
}

// savedCursorMachine is the stash pairing protocol: every enter stashes the
// primary slot (all three alt-screen modes share the per-buffer DECSC
// semantics; only the 1049 cursor save/restore axis differs, and that axis is
// altCursorX/Y, not this stash). Every exit — 1049 and 47/1047 alike —
// restores it. A tmux reattach reset discards without installing (the slot
// was reseeded from the restored cursor by then). A second enter while
// already in alt is a no-op, so the stash survives.
var savedCursorMachine = fsm.MustCompile(fsm.Machine[savedCursorState, savedCursorEvent]{
	Name:    "alt-screen-saved-cursor-stash",
	Initial: cursorLive,
	Steps: []fsm.Step[savedCursorState, savedCursorEvent]{
		{From: cursorLive, Event: evtEnterAlt1049, To: cursorStashed},
		{From: cursorLive, Event: evtEnterAlt47, To: cursorStashed},
		{From: cursorLive, Event: evtExitAlt1049, To: cursorLive},
		{From: cursorLive, Event: evtExitAlt47, To: cursorLive},
		{From: cursorLive, Event: evtReattachReset, To: cursorLive},
		{From: cursorLive, Event: evtDecsc, To: cursorLive},
		{From: cursorLive, Event: evtDecrc, To: cursorLive},
		{From: cursorStashed, Event: evtEnterAlt1049, To: cursorStashed},
		{From: cursorStashed, Event: evtEnterAlt47, To: cursorStashed},
		{From: cursorStashed, Event: evtExitAlt1049, To: cursorLive},
		{From: cursorStashed, Event: evtExitAlt47, To: cursorLive},
		{From: cursorStashed, Event: evtReattachReset, To: cursorLive},
		{From: cursorStashed, Event: evtDecsc, To: cursorStashed},
		{From: cursorStashed, Event: evtDecrc, To: cursorStashed},
	},
})

// TestSavedCursorMachineFormallyChecked verifies the stash table: only an
// enter may create a stash, every exit and every reattach reset retires it,
// and DECSC/DECRC never move it.
func TestSavedCursorMachineFormallyChecked(t *testing.T) {
	err := savedCursorMachine.Check(fsm.Checker[savedCursorState, savedCursorEvent]{
		Alphabet: []savedCursorEvent{
			evtEnterAlt1049, evtEnterAlt47, evtExitAlt1049, evtExitAlt47,
			evtReattachReset, evtDecsc, evtDecrc,
		},
		AllDeclaredReachable: true,
		Edge: func(from savedCursorState, event savedCursorEvent, to savedCursorState) error {
			if to == cursorStashed && from == cursorLive && event != evtEnterAlt1049 && event != evtEnterAlt47 {
				return fmt.Errorf("only an alt-screen enter may stash the primary slot (got %v)", event)
			}
			if from == cursorStashed && to == cursorStashed && event != evtEnterAlt1049 && event != evtEnterAlt47 && event != evtDecsc && event != evtDecrc {
				return fmt.Errorf("event %v left a stash installed after alt screen ended", event)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("saved-cursor stash machine failed model checking: %v", err)
	}
}

// driveSavedCursorEvent applies one protocol event to a real VTerm.
func driveSavedCursorEvent(v *VTerm, event savedCursorEvent) {
	switch event {
	case evtEnterAlt1049:
		v.Write([]byte("\x1b[?1049h"))
	case evtEnterAlt47:
		v.Write([]byte("\x1b[?47h"))
	case evtExitAlt1049:
		v.Write([]byte("\x1b[?1049l"))
	case evtExitAlt47:
		v.Write([]byte("\x1b[?47l"))
	case evtReattachReset:
		// The pane-mode reset a reattach performs when the mode reset never
		// arrived (vterm_scroll_capture.go's applyPaneModeState).
		v.applyPaneModeState(PaneModeState{})
	case evtDecsc:
		v.Write([]byte("\x1b7"))
	case evtDecrc:
		v.Write([]byte("\x1b8"))
	}
}

// TestSavedCursorRuntimeMatchesMachine differentially verifies the shipped
// stash/restore/discard paths against the protocol machine over every event
// sequence of length <= 4: the runtime flag must track the model exactly,
// restores must install the stashed slot, restores while live must be no-ops,
// discards must leave the live slot alone, and a DECSC inside the alt screen
// must never touch the stashed primary slot.
func TestSavedCursorRuntimeMatchesMachine(t *testing.T) {
	events := savedCursorMachine.Events()

	runtimeState := func(v *VTerm) savedCursorState {
		if v.inAltSavedCursor {
			return cursorStashed
		}
		return cursorLive
	}

	var seq []savedCursorEvent
	var check func(depth int)
	check = func(depth int) {
		model := cursorLive
		v := New(10, 4)
		// Seed the primary slot with an identifiable position so restores and
		// no-op paths are distinguishable from a zeroed install.
		v.Write([]byte("hi"))
		v.Write([]byte("\x1b7"))
		if v.SavedCursorX != 2 || v.SavedCursorY != 0 {
			t.Fatalf("seed: saved cursor at (%d,%d), want (2,0)", v.SavedCursorX, v.SavedCursorY)
		}
		liveWant := savedCursor{x: v.SavedCursorX, y: v.SavedCursorY, style: v.SavedStyle}
		var stashWant savedCursor

		for _, event := range seq {
			next, ok := savedCursorMachine.Next(model, event)
			if !ok {
				t.Fatalf("total table rejected %v --%v-->", model, event)
			}
			slotBefore := savedCursor{x: v.SavedCursorX, y: v.SavedCursorY, style: v.SavedStyle}
			stashBefore := v.primarySavedCursor

			driveSavedCursorEvent(v, event)

			// Update expectations per the protocol before comparing.
			if event == evtDecsc {
				// DECSC writes the current buffer's slot (the live one,
				// whichever buffer that is); it never touches a stash.
				liveWant = savedCursor{x: v.SavedCursorX, y: v.SavedCursorY, style: v.SavedStyle}
			}
			switch {
			case next == cursorStashed && model == cursorLive:
				// An enter stashes whatever the live slot held.
				stashWant, liveWant = slotBefore, savedCursor{}
			case next == cursorLive && stashBefore != (savedCursor{}):
				// A paired exit installs the stash; a reattach reset drops it
				// and leaves the live slot alone.
				liveWant = slotBefore
				if event == evtExitAlt1049 || event == evtExitAlt47 {
					liveWant = stashBefore
				}
				stashWant = savedCursor{}
			}
			model = next

			if got := runtimeState(v); got != model {
				t.Errorf("sequence %v: after %v runtime stash state = %v, model says %v", seq, event, got, model)
				return
			}
			if model == cursorStashed {
				if v.primarySavedCursor != stashWant {
					t.Errorf("sequence %v: after %v stashed slot = %+v, want %+v", seq, event, v.primarySavedCursor, stashWant)
				}
			} else {
				if v.primarySavedCursor != (savedCursor{}) {
					t.Errorf("sequence %v: after %v stash still installed: %+v", seq, event, v.primarySavedCursor)
				}
				got := savedCursor{x: v.SavedCursorX, y: v.SavedCursorY, style: v.SavedStyle}
				if got != liveWant {
					t.Errorf("sequence %v: after %v live DECSC slot = %+v, want %+v", seq, event, got, liveWant)
				}
			}
		}

		if depth < 4 {
			for _, event := range events {
				seq = append(seq, event)
				check(depth + 1)
				seq = seq[:len(seq)-1]
			}
		}
	}
	check(0)
}
