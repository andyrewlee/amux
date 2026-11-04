package vterm

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// clear3LatchState is the preserveScrollbackOnNextClear3 one-shot latch's
// control state. The latch defers a CSI 3J scrollback wipe when a redraw
// sequence captured a fresh frame under DEC 2026 sync: the 2J captured, and
// the 3J that follows must preserve what was just captured.
type clear3LatchState int

const (
	latchUnarmed clear3LatchState = iota
	latchArmed
)

func (s clear3LatchState) String() string {
	if s == latchArmed {
		return "armed"
	}
	return "unarmed"
}

// clear3LatchEvent is the latch's alphabet. evtArm is a capturing 2J/0J
// redraw with the sync window open (the guard lives at the call site: only
// syncActive && captured raises it); evtCsiJ3 is the CSI 3J it waits for;
// evtCsiOther is any other CSI dispatch; evtParserReset is any ground or
// non-CSI escape byte plus RIS, all of which break the redraw sequence.
type clear3LatchEvent int

const (
	evtArm clear3LatchEvent = iota
	evtCsiJ3
	evtCsiOther
	evtParserReset
)

func (e clear3LatchEvent) String() string {
	switch e {
	case evtArm:
		return "arm"
	case evtCsiJ3:
		return "csi-j3"
	case evtCsiOther:
		return "csi-other"
	default:
		return "parser-reset"
	}
}

// clear3LatchMachine is the one-shot protocol: only an arming capture raises
// the latch; the first non-arm parser event — the awaited CSI 3J, any other
// CSI, or a sequence break — consumes it. Nothing else can leave it armed.
var clear3LatchMachine = fsm.MustCompile(fsm.Machine[clear3LatchState, clear3LatchEvent]{
	Name:    "clear3-preserve-latch",
	Initial: latchUnarmed,
	Steps: []fsm.Step[clear3LatchState, clear3LatchEvent]{
		{From: latchUnarmed, Event: evtArm, To: latchArmed},
		{From: latchUnarmed, Event: evtCsiJ3, To: latchUnarmed},
		{From: latchUnarmed, Event: evtCsiOther, To: latchUnarmed},
		{From: latchUnarmed, Event: evtParserReset, To: latchUnarmed},
		// A second capturing redraw re-arms (the sequence restarted).
		{From: latchArmed, Event: evtArm, To: latchArmed},
		{From: latchArmed, Event: evtCsiJ3, To: latchUnarmed},
		{From: latchArmed, Event: evtCsiOther, To: latchUnarmed},
		{From: latchArmed, Event: evtParserReset, To: latchUnarmed},
	},
})

// TestClear3LatchMachineFormallyChecked verifies the latch table: armed is
// reachable, only an arm may arm it, and every non-arm event disarms it —
// the latch is one-shot by construction.
func TestClear3LatchMachineFormallyChecked(t *testing.T) {
	err := clear3LatchMachine.Check(fsm.Checker[clear3LatchState, clear3LatchEvent]{
		Alphabet:             []clear3LatchEvent{evtArm, evtCsiJ3, evtCsiOther, evtParserReset},
		AllDeclaredReachable: true,
		Edge: func(from clear3LatchState, event clear3LatchEvent, to clear3LatchState) error {
			if to == latchArmed && event != evtArm {
				return fmt.Errorf("only an arming capture may raise the latch (got %v)", event)
			}
			if from == latchArmed && to == latchArmed && event != evtArm {
				return fmt.Errorf("event %v left the one-shot latch armed", event)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("clear3 latch machine failed model checking: %v", err)
	}
}

// driveClear3LatchEvent applies one latch event to a real VTerm configured
// for normal-screen capture. evtArm's driver repaints content, opens the
// sync window, and issues the capturing CSI 2J — in that order, because
// content bytes and the sync CSI each count as sequence breaks, so the 2J
// must be the sequence's last byte for the arm to take.
func driveClear3LatchEvent(v *VTerm, event clear3LatchEvent) {
	switch event {
	case evtArm:
		v.Write([]byte("hi\x1b[?2026h\x1b[2J"))
	case evtCsiJ3:
		v.Write([]byte("\x1b[3J"))
	case evtCsiOther:
		v.Write([]byte("\x1b[5G"))
	case evtParserReset:
		v.Write([]byte("x"))
	}
}

// TestClear3LatchRuntimeMatchesMachine differentially verifies the scattered
// latch writes (ops.go arming, csi.go consume/clear, parser.go resets)
// against the machine over every event sequence of length <= 4.
func TestClear3LatchRuntimeMatchesMachine(t *testing.T) {
	events := clear3LatchMachine.Events()

	runtimeState := func(v *VTerm) clear3LatchState {
		if v.preserveScrollbackOnNextClear3 {
			return latchArmed
		}
		return latchUnarmed
	}

	var seq []clear3LatchEvent
	var check func(depth int)
	check = func(depth int) {
		v := New(20, 5)
		v.CaptureNormalScreenOnClear = true
		model := latchUnarmed
		for _, event := range seq {
			driveClear3LatchEvent(v, event)
			next, ok := clear3LatchMachine.Next(model, event)
			if !ok {
				t.Fatalf("total table rejected %v --%v-->", model, event)
			}
			model = next
			if got := runtimeState(v); got != model {
				t.Errorf("sequence %v: after %v runtime latch = %v, model says %v", seq, event, got, model)
				return
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

// TestClear3LatchArmGuard pins the arming guard itself: a 2J only raises the
// latch when the sync window is open AND the screen had a capturable frame.
// Neither half alone arms it.
func TestClear3LatchArmGuard(t *testing.T) {
	newTerm := func() *VTerm {
		v := New(20, 5)
		v.CaptureNormalScreenOnClear = true
		return v
	}

	v := newTerm()
	v.Write([]byte("hi\x1b[2J")) // content, but no sync window
	if v.preserveScrollbackOnNextClear3 {
		t.Fatal("2J without sync armed the latch")
	}

	v = newTerm()
	v.Write([]byte("\x1b[?2026h\x1b[2J")) // sync, but blank screen: nothing captured
	if v.preserveScrollbackOnNextClear3 {
		t.Fatal("2J over a blank screen armed the latch")
	}

	v = newTerm()
	v.Write([]byte("hi\x1b[?2026h\x1b[2J"))
	if !v.preserveScrollbackOnNextClear3 {
		t.Fatal("2J over captured content under sync failed to arm the latch")
	}
}

// TestClear3LatchOneShotPreservesScrollback pins the payoff: an armed 3J
// preserves the scrollback the 2J just captured, while an unguarded 3J
// wipes it.
func TestClear3LatchOneShotPreservesScrollback(t *testing.T) {
	seedScrollback := func(v *VTerm) {
		for i := 0; i < v.Height; i++ {
			v.Write([]byte("line\n"))
		}
		v.Write([]byte("more\n"))
	}

	v := New(20, 5)
	v.CaptureNormalScreenOnClear = true
	seedScrollback(v)
	v.Write([]byte("frame\x1b[?2026h\x1b[2J\x1b[3J"))
	if v.preserveScrollbackOnNextClear3 {
		t.Fatal("CSI 3J left the one-shot latch armed")
	}
	if len(v.Scrollback) == 0 {
		t.Fatal("armed CSI 3J wiped the scrollback the 2J just captured")
	}

	v = New(20, 5)
	v.CaptureNormalScreenOnClear = true
	seedScrollback(v)
	v.Write([]byte("\x1b[3J"))
	if len(v.Scrollback) != 0 {
		t.Fatalf("unguarded CSI 3J left %d scrollback lines, want a wipe", len(v.Scrollback))
	}
}
