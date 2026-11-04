package sidebar

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// TestSidebarInputStateDifferential walks the input-binding protocol through
// the real struct operations and asserts the derived State() matches the
// checked machine after every step: bind installs a writer only over
// unbound, a failed send latches on the live binding, retire always unbinds
// (and is the only unlatch), and a failure stamped under a retired gen is
// dropped rather than latched.
func TestSidebarInputStateDifferential(t *testing.T) {
	m := NewTerminalModel() // msgSink nil: failures log, no message posted
	ts := &TerminalState{}
	if got := ts.input.state(); got != inputUnbound {
		t.Fatalf("fresh binding = %v, want unbound", got)
	}
	step := func(ev inputBindingEvent, want inputBindingState, label string) {
		t.Helper()
		gotState, ok := inputBindingMachine.Next(want, ev)
		if !ok {
			t.Fatalf("%s: machine rejected %v from %v", label, ev, want)
		}
		if got := ts.input.state(); got != gotState {
			t.Fatalf("%s: derived state %v, machine says %v", label, got, gotState)
		}
	}

	// Bind: admission lazily installs a writer under the current gen.
	ts.input.writer = ptyio.NewInputWriter(1, func(string) error { return nil }, ts.input.gen, ptyio.InputWriterHooks{})
	ts.input.gen = 1
	step(evtInputBind, inputUnbound, "bind")

	// Send failure on the live binding latches.
	ts.input.failed = true
	step(evtInputSendFail, inputBound, "send failure latches")

	// Retire unbinds, bumps the gen, and clears the latch.
	retired := ts.retireSidebarInputWriterLocked()
	if retired == nil {
		t.Fatal("retire must return the writer for joining")
	}
	if ts.input.gen != 2 {
		t.Fatalf("retire bumped gen to %d, want 2", ts.input.gen)
	}
	step(evtInputRetire, inputFailed, "retire unlatches and unbinds")

	// A late failure stamped under the retired gen must not latch the
	// (now unbound) state — failSidebarInput drops it on writer mismatch.
	failSidebarInput(ts, m, retired, ptyio.InputRequest{Label: "stale", Gen: 1}, errors.New("late"), "ws", "tab")
	if got := ts.input.state(); got != inputUnbound {
		t.Fatalf("stale failure latched %v, want unbound", got)
	}

	// Re-bind on the next admission, then a failure that belongs to the
	// live writer latches again.
	ts.input.writer = ptyio.NewInputWriter(1, func(string) error { return nil }, ts.input.gen, ptyio.InputWriterHooks{})
	ts.input.gen = 3
	step(evtInputBind, inputUnbound, "re-bind")
	failSidebarInput(ts, m, ts.input.writer, ptyio.InputRequest{Label: "live", Gen: ts.input.writer.Gen}, errors.New("now"), "ws", "tab")
	step(evtInputSendFail, inputBound, "live failure latches")
}
