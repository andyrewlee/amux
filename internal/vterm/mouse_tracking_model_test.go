package vterm

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// mouseState is the mouse-tracking mode selector (vterm.go's mouseTrackingMode
// int). The SGR-format flag (mode 1006) is orthogonal and modeled below.
type mouseState int

const (
	mouseOff mouseState = iota
	mouseNormal
	mouseDrag
	mouseAny
)

func (s mouseState) String() string {
	switch s {
	case mouseNormal:
		return "normal-1000"
	case mouseDrag:
		return "drag-1002"
	case mouseAny:
		return "any-1003"
	default:
		return "off"
	}
}

// mouseEvent is the selector's alphabet: set/unset per mode plus RIS reset.
type mouseEvent int

const (
	evtMouseSet1000 mouseEvent = iota
	evtMouseSet1002
	evtMouseSet1003
	evtMouseUnset1000
	evtMouseUnset1002
	evtMouseUnset1003
	evtMouseReset
)

func (e mouseEvent) String() string {
	switch e {
	case evtMouseSet1000:
		return "set-1000"
	case evtMouseSet1002:
		return "set-1002"
	case evtMouseSet1003:
		return "set-1003"
	case evtMouseUnset1000:
		return "unset-1000"
	case evtMouseUnset1002:
		return "unset-1002"
	case evtMouseUnset1003:
		return "unset-1003"
	default:
		return "reset"
	}
}

// mouseMachine is the selector protocol: a set assigns (last writer wins —
// the modes are mutually exclusive variants); an unset clears only its own
// mode and is a no-op for the others; RIS always lands off. Runtime
// mouseTrackingMode is a plain int assignment on this hot, per-frame-read
// path, so this stays a verification-only model rather than a runtime enum.
var mouseMachine = fsm.MustCompile(fsm.Machine[mouseState, mouseEvent]{
	Name:    "mouse-tracking-mode",
	Initial: mouseOff,
	Steps: []fsm.Step[mouseState, mouseEvent]{
		{From: mouseOff, Event: evtMouseSet1000, To: mouseNormal},
		{From: mouseOff, Event: evtMouseSet1002, To: mouseDrag},
		{From: mouseOff, Event: evtMouseSet1003, To: mouseAny},
		{From: mouseOff, Event: evtMouseUnset1000, To: mouseOff},
		{From: mouseOff, Event: evtMouseUnset1002, To: mouseOff},
		{From: mouseOff, Event: evtMouseUnset1003, To: mouseOff},
		{From: mouseOff, Event: evtMouseReset, To: mouseOff},

		{From: mouseNormal, Event: evtMouseSet1000, To: mouseNormal},
		{From: mouseNormal, Event: evtMouseSet1002, To: mouseDrag},
		{From: mouseNormal, Event: evtMouseSet1003, To: mouseAny},
		{From: mouseNormal, Event: evtMouseUnset1000, To: mouseOff},
		{From: mouseNormal, Event: evtMouseUnset1002, To: mouseNormal},
		{From: mouseNormal, Event: evtMouseUnset1003, To: mouseNormal},
		{From: mouseNormal, Event: evtMouseReset, To: mouseOff},

		{From: mouseDrag, Event: evtMouseSet1000, To: mouseNormal},
		{From: mouseDrag, Event: evtMouseSet1002, To: mouseDrag},
		{From: mouseDrag, Event: evtMouseSet1003, To: mouseAny},
		{From: mouseDrag, Event: evtMouseUnset1000, To: mouseDrag},
		{From: mouseDrag, Event: evtMouseUnset1002, To: mouseOff},
		{From: mouseDrag, Event: evtMouseUnset1003, To: mouseDrag},
		{From: mouseDrag, Event: evtMouseReset, To: mouseOff},

		{From: mouseAny, Event: evtMouseSet1000, To: mouseNormal},
		{From: mouseAny, Event: evtMouseSet1002, To: mouseDrag},
		{From: mouseAny, Event: evtMouseSet1003, To: mouseAny},
		{From: mouseAny, Event: evtMouseUnset1000, To: mouseAny},
		{From: mouseAny, Event: evtMouseUnset1002, To: mouseAny},
		{From: mouseAny, Event: evtMouseUnset1003, To: mouseOff},
		{From: mouseAny, Event: evtMouseReset, To: mouseOff},
	},
})

// TestMouseMachineFormallyChecked verifies the selector table: every mode is
// reachable, reset always lands off, a set always lands on its own mode, and
// an unset of another mode never moves the state.
func TestMouseMachineFormallyChecked(t *testing.T) {
	err := mouseMachine.Check(fsm.Checker[mouseState, mouseEvent]{
		Alphabet:             mouseMachine.Events(),
		AllDeclaredReachable: true,
		Edge: func(from mouseState, event mouseEvent, to mouseState) error {
			if event == evtMouseReset && to != mouseOff {
				return fmt.Errorf("reset landed on %v, want off", to)
			}
			if event == evtMouseSet1000 && to != mouseNormal {
				return fmt.Errorf("set-1000 landed on %v", to)
			}
			if event == evtMouseSet1002 && to != mouseDrag {
				return fmt.Errorf("set-1002 landed on %v", to)
			}
			if event == evtMouseSet1003 && to != mouseAny {
				return fmt.Errorf("set-1003 landed on %v", to)
			}
			switch event {
			case evtMouseUnset1000:
				if from != mouseNormal && to != from {
					return fmt.Errorf("unset-1000 from %v moved to %v: unset-of-other-mode must be a no-op", from, to)
				}
			case evtMouseUnset1002:
				if from != mouseDrag && to != from {
					return fmt.Errorf("unset-1002 from %v moved to %v: unset-of-other-mode must be a no-op", from, to)
				}
			case evtMouseUnset1003:
				if from != mouseAny && to != from {
					return fmt.Errorf("unset-1003 from %v moved to %v: unset-of-other-mode must be a no-op", from, to)
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("mouse tracking machine failed model checking: %v", err)
	}
}

// TestMouseRuntimeMatchesMachine differentially verifies
// setMouseTrackingMode and the RIS reset against the machine over every
// event sequence of length <= 3, with the SGR flag exercised in parallel.
func TestMouseRuntimeMatchesMachine(t *testing.T) {
	runtimeState := func(v *VTerm) mouseState {
		switch v.mouseTrackingMode {
		case 1000:
			return mouseNormal
		case 1002:
			return mouseDrag
		case 1003:
			return mouseAny
		default:
			return mouseOff
		}
	}

	events := mouseMachine.Events()
	var seq []mouseEvent
	var check func(depth int)
	check = func(depth int) {
		v := New(20, 4)
		v.Write([]byte("\x1b[?1006h")) // SGR flag on: it must never interact
		model := mouseOff
		sgrWant := true
		for _, event := range seq {
			switch event {
			case evtMouseSet1000:
				v.Write([]byte("\x1b[?1000h"))
			case evtMouseSet1002:
				v.Write([]byte("\x1b[?1002h"))
			case evtMouseSet1003:
				v.Write([]byte("\x1b[?1003h"))
			case evtMouseUnset1000:
				v.Write([]byte("\x1b[?1000l"))
			case evtMouseUnset1002:
				v.Write([]byte("\x1b[?1002l"))
			case evtMouseUnset1003:
				v.Write([]byte("\x1b[?1003l"))
			case evtMouseReset:
				v.Write([]byte("\x1bc"))
				sgrWant = false
			}
			next, ok := mouseMachine.Next(model, event)
			if !ok {
				t.Fatalf("total table rejected %v --%v-->", model, event)
			}
			model = next
			if got := runtimeState(v); got != model {
				t.Errorf("sequence %v: after %v runtime mode = %v, model says %v", seq, event, got, model)
				return
			}
			if v.mouseSGRMode != sgrWant {
				t.Errorf("sequence %v: after %v SGR flag = %v, want %v (only RIS may clear it)", seq, event, v.mouseSGRMode, sgrWant)
			}
		}
		if depth < 3 {
			for _, event := range events {
				seq = append(seq, event)
				check(depth + 1)
				seq = seq[:len(seq)-1]
			}
		}
	}
	check(0)
}

// TestMouseSGRFlagIndependent pins the SGR flag's independence from the
// selector: it toggles freely across mode changes and resets only on RIS.
func TestMouseSGRFlagIndependent(t *testing.T) {
	v := New(20, 4)
	v.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !v.MouseSGRMode() || !v.MouseReportingEnabled() {
		t.Fatal("1000+1006 failed to enable both")
	}
	v.Write([]byte("\x1b[?1000l")) // unset tracking; SGR must survive
	if v.MouseReportingEnabled() {
		t.Fatal("unset-1000 left reporting enabled")
	}
	if !v.MouseSGRMode() {
		t.Fatal("unset-1000 cleared the SGR flag")
	}
	v.Write([]byte("\x1bc"))
	if v.MouseSGRMode() {
		t.Fatal("RIS left the SGR flag set")
	}
}
