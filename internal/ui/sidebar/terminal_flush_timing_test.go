package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// Background sidebar terminals keep streaming while another tab is selected, so
// flush handlers must time the terminal they are flushing. Timing by the
// selected tab hands an alt-screen terminal the faster non-alt cadence, and the
// shared flush policy reads the quiet period to tell whether the caller asked
// for a slower one (ptyio.FlushDelay gates its early frame flush on it).
func TestFlushTimingForUsesTheGivenTerminal(t *testing.T) {
	m := newTerminalModelWithWorkspace(t)
	wsID := m.workspaceID()

	altTerm := vterm.New(80, 24)
	altTerm.Write([]byte("\x1b[?1049h")) // enter alt screen
	if !altTerm.AltScreen {
		t.Fatal("setup: terminal did not enter alt screen")
	}
	background := &TerminalTab{ID: "bg", Name: "bg", State: &TerminalState{VTerm: altTerm}}
	selected := &TerminalTab{ID: "sel", Name: "sel", State: &TerminalState{VTerm: vterm.New(80, 24)}}

	m.tabs.ByWorkspace[wsID] = []*TerminalTab{background, selected}
	m.tabs.ActiveByWorkspace[wsID] = 1 // the non-alt tab is on screen

	quiet, maxInterval := m.flushTimingFor(background.State)
	if quiet != ptyFlushQuietAlt || maxInterval != ptyFlushMaxAlt {
		t.Fatalf("background alt terminal timed as (%v, %v), want alt timing (%v, %v)",
			quiet, maxInterval, ptyFlushQuietAlt, ptyFlushMaxAlt)
	}

	if quiet, _ := m.flushTimingFor(selected.State); quiet != ptyFlushQuiet {
		t.Fatalf("selected non-alt terminal timed as %v, want %v", quiet, ptyFlushQuiet)
	}
}
