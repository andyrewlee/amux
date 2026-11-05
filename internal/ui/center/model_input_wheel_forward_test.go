package center

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/vterm"
)

// The wheel contract for alternate-screen panes: the hosted application owns
// its viewport, so the wheel is delivered to it — as mouse sequences when it
// reports the mouse, or as cursor-key presses when it does not (tmux's own
// alternate-scroll behavior) — and never scrolls a local, synthesized
// history. The pane's screen state is the host-managed probe flag.

func TestMouseWheelInputSequenceAlternateScrollBecomesCursorKeys(t *testing.T) {
	term := vterm.New(80, 24)
	term.SetPaneAlternateScreen(true)

	if got, want := mouseWheelInputSequence(term, tea.MouseWheelUp, 5, 5), "\x1b[A\x1b[A\x1b[A"; got != want {
		t.Fatalf("wheel up = %q, want %q (CSI arrows, 3 per notch)", got, want)
	}
	if got, want := mouseWheelInputSequence(term, tea.MouseWheelDown, 5, 5), "\x1b[B\x1b[B\x1b[B"; got != want {
		t.Fatalf("wheel down = %q, want %q", got, want)
	}

	term.Write([]byte("\x1b[?1h"))
	if got, want := mouseWheelInputSequence(term, tea.MouseWheelUp, 5, 5), "\x1bOA\x1bOA\x1bOA"; got != want {
		t.Fatalf("wheel up with DECCKM = %q, want %q (SS3 arrows)", got, want)
	}
}

func TestMouseWheelInputSequenceMouseReportingWinsOverAlternateScroll(t *testing.T) {
	term := vterm.New(80, 24)
	term.SetPaneAlternateScreen(true)
	term.Write([]byte("\x1b[?1006h\x1b[?1000h"))

	if got, want := mouseWheelInputSequence(term, tea.MouseWheelUp, 5, 5), "\x1b[<64;6;6M"; got != want {
		t.Fatalf("wheel up = %q, want SGR sequence %q", got, want)
	}
}

func TestMouseWheelInputSequenceNoConversionOutsideAlternateScreen(t *testing.T) {
	term := vterm.New(80, 24)
	// Primary screen: the wheel belongs to amux's own scrollback.
	if got := mouseWheelInputSequence(term, tea.MouseWheelUp, 5, 5); got != "" {
		t.Fatalf("primary-screen wheel = %q, want empty", got)
	}
}

func TestMouseWheelDoesNotScrollAltScreenPaneLocally(t *testing.T) {
	agent, read := newWheelCaptureAgent(t)
	m, tab := setupSelectionModel(t)
	m.showKeymapHints = false
	tab.mu.Lock()
	tab.Agent = agent
	tab.Terminal.SetPaneAlternateScreen(true)
	tab.paneStateProbedAt = time.Now() // fresh: the inline path decides, no probe
	tab.Terminal.Write([]byte(strings.Repeat("line\n", 30)))
	tab.mu.Unlock()

	tm := m.terminalMetrics()
	m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelUp,
		X:      tm.ContentStartX + 2,
		Y:      tm.ContentStartY + 1,
	})

	// The wheel must reach the hosted app — otherwise "no local scroll"
	// would hold trivially, with the event simply dropped.
	waitForSent(t, read, "\x1b[A\x1b[A\x1b[A")

	tab.mu.Lock()
	offset, maxOffset := tab.Terminal.GetScrollInfo()
	scrollback := len(tab.Terminal.Scrollback)
	tab.mu.Unlock()
	if offset != 0 || maxOffset != 0 || scrollback != 0 {
		t.Fatalf("alt-screen pane must keep no local history, got offset=%d max=%d scrollback=%d", offset, maxOffset, scrollback)
	}
}

func TestMouseWheelScrollsPrimaryScreenScrollback(t *testing.T) {
	m, tab := setupSelectionModel(t)
	m.showKeymapHints = false
	tab.mu.Lock()
	tab.Terminal.Write([]byte(strings.Repeat("line\n", 40)))
	tab.mu.Unlock()

	tm := m.terminalMetrics()
	m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelUp,
		X:      tm.ContentStartX + 2,
		Y:      tm.ContentStartY + 1,
	})

	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset == 0 {
		t.Fatal("expected wheel to scroll primary-screen scrollback")
	}
}
