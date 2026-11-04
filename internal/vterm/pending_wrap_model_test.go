package vterm

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// wrapState is the deferred-wrap automaton's control state. wrapClear: the
// cursor sits on a cell it owns. wrapPending: the last putChar filled the
// final column (CursorX == Width) and the wrap is deferred until the next
// print — cursor-addressing ops, screen switches, and resizes cancel it;
// erase ops and cursor reports do not.
type wrapState int

const (
	wrapClear wrapState = iota
	wrapPending
)

func (s wrapState) String() string {
	if s == wrapPending {
		return "wrap-pending"
	}
	return "wrap-clear"
}

// wrapEvent is the deferred-wrap alphabet, one event per byte-level op class
// that touches the flag.
type wrapEvent int

const (
	evtWrapPrintLastCol wrapEvent = iota
	evtWrapPrintMidRow
	evtWrapCR
	evtWrapLF
	evtWrapBS
	evtWrapCHA
	evtWrapCursorAbsolute
	evtWrapCursorRelative
	evtWrapErase
	evtWrapDSR
	evtWrapEnterAlt
	evtWrapExitAlt
	evtWrapResize
)

func (e wrapEvent) String() string {
	switch e {
	case evtWrapPrintLastCol:
		return "print-last-col"
	case evtWrapPrintMidRow:
		return "print-mid-row"
	case evtWrapCR:
		return "cr"
	case evtWrapLF:
		return "lf"
	case evtWrapBS:
		return "bs"
	case evtWrapCHA:
		return "cha"
	case evtWrapCursorAbsolute:
		return "cursor-absolute"
	case evtWrapCursorRelative:
		return "cursor-relative"
	case evtWrapErase:
		return "erase"
	case evtWrapDSR:
		return "dsr"
	case evtWrapEnterAlt:
		return "enter-alt"
	case evtWrapExitAlt:
		return "exit-alt"
	default:
		return "resize"
	}
}

// wrapMachine is the deferred-wrap protocol. Only a print that lands exactly
// on the last column raises the flag; any print while pending fires the
// deferred wrap (and the landing cell is never the last column of a width>1
// screen, so the flag drops). Every cursor-addressing op, LF, screen switch,
// and resize clears it. Erase ops and DSR are reads, not cursor moves: they
// self-loop in both states (pinned by vterm_pending_wrap_test.go).
//
// BS is the deliberate exception: backspace does not clear the flag — it
// only decrements the raw column counter (Width -> Width-1) and leaves the
// pending state intact, so a following print re-arms it after moving one
// cell left of the edge. xterm cancels the pending wrap on BS; this
// emulator keeps it, which is observable only as wrap-on-next-print firing
// one column early after a BS at the edge.
var wrapMachine = fsm.MustCompile(fsm.Machine[wrapState, wrapEvent]{
	Name:    "deferred-pending-wrap",
	Initial: wrapClear,
	Steps: []fsm.Step[wrapState, wrapEvent]{
		{From: wrapClear, Event: evtWrapPrintLastCol, To: wrapPending},
		{From: wrapClear, Event: evtWrapPrintMidRow, To: wrapClear},
		{From: wrapClear, Event: evtWrapCR, To: wrapClear},
		{From: wrapClear, Event: evtWrapLF, To: wrapClear},
		{From: wrapClear, Event: evtWrapBS, To: wrapClear},
		{From: wrapClear, Event: evtWrapCHA, To: wrapClear},
		{From: wrapClear, Event: evtWrapCursorAbsolute, To: wrapClear},
		{From: wrapClear, Event: evtWrapCursorRelative, To: wrapClear},
		{From: wrapClear, Event: evtWrapErase, To: wrapClear},
		{From: wrapClear, Event: evtWrapDSR, To: wrapClear},
		{From: wrapClear, Event: evtWrapEnterAlt, To: wrapClear},
		{From: wrapClear, Event: evtWrapExitAlt, To: wrapClear},
		{From: wrapClear, Event: evtWrapResize, To: wrapClear},

		{From: wrapPending, Event: evtWrapPrintLastCol, To: wrapClear},
		{From: wrapPending, Event: evtWrapPrintMidRow, To: wrapClear},
		{From: wrapPending, Event: evtWrapCR, To: wrapClear},
		{From: wrapPending, Event: evtWrapLF, To: wrapClear},
		{From: wrapPending, Event: evtWrapBS, To: wrapPending},
		{From: wrapPending, Event: evtWrapCHA, To: wrapClear},
		{From: wrapPending, Event: evtWrapCursorAbsolute, To: wrapClear},
		{From: wrapPending, Event: evtWrapCursorRelative, To: wrapClear},
		{From: wrapPending, Event: evtWrapErase, To: wrapPending},
		{From: wrapPending, Event: evtWrapDSR, To: wrapPending},
		{From: wrapPending, Event: evtWrapEnterAlt, To: wrapClear},
		{From: wrapPending, Event: evtWrapExitAlt, To: wrapClear},
		{From: wrapPending, Event: evtWrapResize, To: wrapClear},
	},
})

// TestWrapMachineFormallyChecked verifies the deferred-wrap table: pending
// is reachable, only a last-column print raises it, erase and DSR never
// change it, and every cursor-addressing op clears it.
func TestWrapMachineFormallyChecked(t *testing.T) {
	err := wrapMachine.Check(fsm.Checker[wrapState, wrapEvent]{
		Alphabet:             wrapMachine.Events(),
		AllDeclaredReachable: true,
		Edge: func(from wrapState, event wrapEvent, to wrapState) error {
			if to == wrapPending && from == wrapClear && event != evtWrapPrintLastCol {
				return fmt.Errorf("only a last-column print may raise pending wrap (got %v)", event)
			}
			if from == wrapPending && to == wrapPending &&
				event != evtWrapBS && event != evtWrapErase && event != evtWrapDSR {
				return fmt.Errorf("event %v left pending wrap set", event)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("pending-wrap machine failed model checking: %v", err)
	}
}

// TestWrapRuntimeMatchesMachine differentially verifies the shipped flag
// writes against the machine at event grain: for every event from both
// states, a real VTerm driven through the actual parser must land in the
// modeled state.
func TestWrapRuntimeMatchesMachine(t *testing.T) {
	// exitAlt is only observable from inside the alt screen (it early-returns
	// otherwise), so its driver enters first; every other event is a direct
	// byte sequence.
	drive := func(v *VTerm, event wrapEvent) {
		switch event {
		case evtWrapPrintLastCol:
			if v.PendingWrap {
				v.Write([]byte("z"))
			} else {
				v.Write([]byte(fmt.Sprintf("\x1b[%dGz", v.Width)))
			}
		case evtWrapPrintMidRow:
			v.Write([]byte("z"))
		case evtWrapCR:
			v.Write([]byte("\r"))
		case evtWrapLF:
			v.Write([]byte("\n"))
		case evtWrapBS:
			v.Write([]byte("\x08"))
		case evtWrapCHA:
			v.Write([]byte("\x1b[5G"))
		case evtWrapCursorAbsolute:
			v.Write([]byte("\x1b[2;3H"))
		case evtWrapCursorRelative:
			v.Write([]byte("\x1b[2C"))
		case evtWrapErase:
			// EL 0 / EL 2 / ED 0 are the pinned trio; all must behave alike.
			v.Write([]byte("\x1b[K"))
		case evtWrapDSR:
			v.Write([]byte("\x1b[6n"))
		case evtWrapEnterAlt:
			v.Write([]byte("\x1b[?1049h"))
		case evtWrapExitAlt:
			v.Write([]byte("\x1b[?1049l"))
		case evtWrapResize:
			v.Resize(v.Width, v.Height+2)
		}
	}

	runtimeState := func(v *VTerm) wrapState {
		if v.PendingWrap {
			return wrapPending
		}
		return wrapClear
	}

	for _, state := range []wrapState{wrapClear, wrapPending} {
		for _, event := range wrapMachine.Events() {
			modeled, ok := wrapMachine.Next(state, event)
			if !ok {
				continue // total table; nothing is rejected
			}
			// EL 0 / EL 2 / ED 0 are the pinned erase trio; all must behave
			// alike, so the erase event runs once per sequence.
			eventSeqs := [][]byte{nil}
			if event == evtWrapErase {
				eventSeqs = [][]byte{[]byte("\x1b[K"), []byte("\x1b[2K"), []byte("\x1b[J")}
			}
			for _, seq := range eventSeqs {
				v := New(10, 4)
				if event == evtWrapExitAlt {
					// exitAltScreen early-returns off the alt screen, so the
					// driver enters first; the enter itself clears, and the
					// setup below re-establishes the state under test.
					v.Write([]byte("\x1b[?1049h"))
				}
				if state == wrapPending {
					fillRowToEdge(v, "a")
				} else {
					v.Write([]byte("a"))
				}
				if seq == nil {
					drive(v, event)
				} else {
					v.Write(seq)
				}
				if got := runtimeState(v); got != modeled {
					t.Errorf("%v --%v--(seq %q)--> runtime %v, model says %v", state, event, seq, got, modeled)
				}
			}
		}
	}
}
