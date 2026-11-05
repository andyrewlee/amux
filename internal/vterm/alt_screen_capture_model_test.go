package vterm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// captureState is the altScreenCaptureState tracker's qualitative control
// state. capUntracked: no frame position is remembered. capTrackedCurrent:
// a reserved frame sits at the scrollback tail (endOffset == 0).
// capTrackedShifted: scrollUp appended rows behind it (endOffset > 0).
// The numeric fields (frameLen, dropLen, endOffset) are extended state at
// the call site; this machine is the verified skeleton they hang from.
type captureState int

const (
	capUntracked captureState = iota
	capTrackedCurrent
	capTrackedShifted
)

func (s captureState) String() string {
	switch s {
	case capTrackedCurrent:
		return "tracked-current"
	case capTrackedShifted:
		return "tracked-shifted"
	default:
		return "untracked"
	}
}

// captureEvent is the tracker's alphabet. captureNew is a fresh (non-dedup)
// frame append; dedup is a redraw matching the tracked frame (it folds any
// shifted tail back to transcript order, zeroing endOffset); replace and
// transition are the two retirement paths (dropTrackedAltScreenCapture and
// transitionTrackedAltScreenCapture); scrollUp is rows appended behind the
// frame; invalidate is every reset (including the runtime-rejected edges
// resetInvalid logs).
type captureEvent int

const (
	evtCaptureNew captureEvent = iota
	evtCaptureDedup
	evtCaptureReplace
	evtCaptureTransition
	evtCaptureScrollUp
	evtCaptureInvalidate
)

func (e captureEvent) String() string {
	switch e {
	case evtCaptureNew:
		return "capture-new"
	case evtCaptureDedup:
		return "dedup"
	case evtCaptureReplace:
		return "replace"
	case evtCaptureTransition:
		return "transition"
	case evtCaptureScrollUp:
		return "scroll-up"
	default:
		return "invalidate"
	}
}

// captureMachine is the tracker protocol: only a capture creates tracking;
// scrollUp can shift a tracked frame but never track an untracked one; dedup
// normalizes a shifted frame back to the tail; the retirement events and
// invalidation always leave nothing tracked. Untracked dedup/replace/
// transition are table rejections — the runtime paths are no-op resets.
var captureMachine = fsm.MustCompile(fsm.Machine[captureState, captureEvent]{
	Name:    "alt-screen-capture-tracking",
	Initial: capUntracked,
	Steps: []fsm.Step[captureState, captureEvent]{
		{From: capUntracked, Event: evtCaptureNew, To: capTrackedCurrent},
		{From: capUntracked, Event: evtCaptureScrollUp, To: capUntracked},
		{From: capUntracked, Event: evtCaptureInvalidate, To: capUntracked},

		{From: capTrackedCurrent, Event: evtCaptureNew, To: capTrackedCurrent},
		{From: capTrackedCurrent, Event: evtCaptureDedup, To: capTrackedCurrent},
		{From: capTrackedCurrent, Event: evtCaptureReplace, To: capUntracked},
		{From: capTrackedCurrent, Event: evtCaptureTransition, To: capUntracked},
		{From: capTrackedCurrent, Event: evtCaptureScrollUp, To: capTrackedShifted},
		{From: capTrackedCurrent, Event: evtCaptureInvalidate, To: capUntracked},

		{From: capTrackedShifted, Event: evtCaptureNew, To: capTrackedCurrent},
		{From: capTrackedShifted, Event: evtCaptureDedup, To: capTrackedCurrent},
		{From: capTrackedShifted, Event: evtCaptureReplace, To: capUntracked},
		{From: capTrackedShifted, Event: evtCaptureTransition, To: capUntracked},
		{From: capTrackedShifted, Event: evtCaptureScrollUp, To: capTrackedShifted},
		{From: capTrackedShifted, Event: evtCaptureInvalidate, To: capUntracked},
	},
})

// TestCaptureMachineFormallyChecked verifies the tracker table: tracking is
// reachable, only a capture or a dedup-retrack may create it, only scrollUp
// may shift it, and the retirement events always fully untrack.
func TestCaptureMachineFormallyChecked(t *testing.T) {
	err := captureMachine.Check(fsm.Checker[captureState, captureEvent]{
		Alphabet: []captureEvent{
			evtCaptureNew, evtCaptureDedup, evtCaptureReplace,
			evtCaptureTransition, evtCaptureScrollUp, evtCaptureInvalidate,
		},
		AllDeclaredReachable: true,
		Edge: func(from captureState, event captureEvent, to captureState) error {
			if to == capTrackedShifted && event != evtCaptureScrollUp {
				return fmt.Errorf("only scrollUp may shift a tracked frame (got %v)", event)
			}
			if event == evtCaptureScrollUp && to == capTrackedShifted && from == capUntracked {
				return errors.New("scrollUp tracked an untracked frame")
			}
			if to != capUntracked && event != evtCaptureNew && event != evtCaptureDedup && event != evtCaptureScrollUp {
				return fmt.Errorf("retirement event %v left the frame tracked", event)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("alt-screen capture machine failed model checking: %v", err)
	}
}

// assertCaptureLattice checks the numeric invariant lattice documented on
// altScreenCaptureState at every step of a real flow:
//
//   - tracked implies 0 < dropLen <= frameLen
//   - frameLen + endOffset <= len(Scrollback)
//   - endOffset > 0 only while tracked
func assertCaptureLattice(t *testing.T, v *VTerm, step string) {
	t.Helper()
	c := v.altCapture
	if c.tracked {
		if c.dropLen <= 0 || c.dropLen > c.frameLen {
			t.Fatalf("step %q: tracked with dropLen=%d frameLen=%d, want 0 < dropLen <= frameLen", step, c.dropLen, c.frameLen)
		}
	}
	if c.endOffset > 0 && !c.tracked {
		t.Fatalf("step %q: endOffset=%d while untracked", step, c.endOffset)
	}
	if c.frameLen+c.endOffset > len(v.Scrollback) {
		t.Fatalf("step %q: frameLen=%d + endOffset=%d exceeds scrollback len %d", step, c.frameLen, c.endOffset, len(v.Scrollback))
	}
}

// TestCaptureLatticeHoldsOverRealFlow drives the real capture flow — main
// screen with CaptureNormalScreenOnClear, transcript redraws with scrolling
// between them, identical redraws (dedup), a resize, and a final wipe — and
// asserts the numeric invariant lattice holds after every step.
func TestCaptureLatticeHoldsOverRealFlow(t *testing.T) {
	v := New(20, 6)
	v.CaptureNormalScreenOnClear = true

	step := func(name string, data []byte) {
		t.Helper()
		v.Write(data)
		assertCaptureLattice(t, v, name)
	}

	// captureNew: first transcript frame.
	step("first-frame", []byte("alpha\nbeta\ngamma\n\x1b[2J"))
	// scrollUp: redraw content scrolled off the top between captures.
	step("scroll-1", []byte("delta\n"))
	step("frame-2", []byte("alpha\nbeta\ngamma\ndelta\n\x1b[2J"))
	// dedup: identical frame redrawn.
	step("dedup", []byte("alpha\nbeta\ngamma\ndelta\n\x1b[2J"))
	// transition: frame shifts by one row (suffix becomes prefix).
	step("scroll-2", []byte("epsilon\n"))
	step("frame-shift", []byte("beta\ngamma\ndelta\nepsilon\n\x1b[2J"))
	// more scrolling shifts the tracked frame deeper into scrollback.
	step("scroll-3", []byte("zeta\neta\n"))
	step("dedup-shifted", []byte("beta\ngamma\ndelta\nepsilon\n\x1b[2J"))

	// A resize must not break the lattice either.
	v.Resize(20, 8)
	assertCaptureLattice(t, v, "resize")

	// The final 3J wipe invalidates the tracker.
	step("wipe", []byte("\x1b[3J"))
	if v.altCapture.tracked || v.altCapture.frameLen != 0 {
		t.Fatalf("after 3J: tracker = %+v, want fully reset", v.altCapture)
	}
}
