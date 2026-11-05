package vterm

import "testing"

// DECSET 1007 (alternate scroll) and DECCKM (application cursor keys) are the
// two modes that decide how a wheel event is encoded for a full-screen
// application that does not use mouse reporting: as SS3/CSI cursor keys,
// three presses per notch.

func TestAlternateScrollModeTracked(t *testing.T) {
	term := New(80, 24)

	if term.AlternateScrollMode() {
		t.Fatal("expected alternate scroll to start disabled")
	}

	// The pane's screen state is host-managed (probed from tmux), not parsed.
	term.SetPaneAlternateScreen(true)
	if !term.WheelScrollBecomesCursorKeys() {
		t.Fatal("expected wheel to become cursor keys on an alt-screen pane without mouse reporting")
	}

	// Mouse reporting always wins: the application asked for real events.
	term.Write([]byte("\x1b[?1000h"))
	if term.WheelScrollBecomesCursorKeys() {
		t.Fatal("expected mouse reporting to take precedence over alternate scroll")
	}
	term.Write([]byte("\x1b[?1000l"))
	if !term.WheelScrollBecomesCursorKeys() {
		t.Fatal("expected alternate scroll to resume after mouse reporting turns off")
	}

	// Leaving the alternate screen suspends conversion.
	term.SetPaneAlternateScreen(false)
	if term.WheelScrollBecomesCursorKeys() {
		t.Fatal("expected no cursor-key conversion on a primary-screen pane")
	}

	term.Write([]byte("\x1b[?1007h"))
	if !term.AlternateScrollMode() {
		t.Fatal("expected 1007h to enable alternate scroll tracking")
	}
	term.Write([]byte("\x1b[?1007l"))
	if term.AlternateScrollMode() {
		t.Fatal("expected 1007l to disable alternate scroll tracking")
	}
}

func TestAlternateScrollAndCursorKeysClearOnTerminalReset(t *testing.T) {
	term := New(80, 24)
	term.Write([]byte("\x1b[?1007h\x1b[?1h"))
	if !term.AlternateScrollMode() || !term.ApplicationCursorKeys() {
		t.Fatal("expected alternate scroll and application cursor keys enabled")
	}

	term.Write([]byte("\x1bc"))

	if term.AlternateScrollMode() {
		t.Fatal("expected terminal reset to disable alternate scroll")
	}
	if term.ApplicationCursorKeys() {
		t.Fatal("expected terminal reset to restore normal cursor keys")
	}
}

func TestApplicationCursorKeysTracked(t *testing.T) {
	term := New(80, 24)

	term.Write([]byte("\x1b[?1h"))
	if !term.ApplicationCursorKeys() {
		t.Fatal("expected application cursor keys enabled")
	}

	term.Write([]byte("\x1b[?1l"))
	if term.ApplicationCursorKeys() {
		t.Fatal("expected normal cursor keys after DECCKM reset")
	}
}
