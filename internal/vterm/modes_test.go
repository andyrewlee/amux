package vterm

import "testing"

func TestMouseReportingModesTracked(t *testing.T) {
	term := New(80, 24)

	term.Write([]byte("\x1b[?1000h"))
	if !term.MouseReportingEnabled() {
		t.Fatal("expected normal mouse reporting to be enabled")
	}
	if term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to start disabled")
	}

	term.Write([]byte("\x1b[?1006h"))
	if !term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to be enabled")
	}

	term.Write([]byte("\x1b[?1000l"))
	if term.MouseReportingEnabled() {
		t.Fatal("expected mouse reporting to be disabled")
	}
	if !term.MouseSGRMode() {
		t.Fatal("expected SGR coordinate mode to remain tracked independently")
	}

	term.Write([]byte("\x1b[?1006l"))
	if term.MouseSGRMode() {
		t.Fatal("expected SGR mouse mode to be disabled")
	}
}

func TestMouseReportingModesClearOnTerminalReset(t *testing.T) {
	term := New(80, 24)
	term.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !term.MouseReportingEnabled() || !term.MouseSGRMode() {
		t.Fatal("expected mouse reporting modes to start enabled")
	}

	term.Write([]byte("\x1bc"))

	if term.MouseReportingEnabled() {
		t.Fatal("expected terminal reset to disable mouse reporting")
	}
	if term.MouseSGRMode() {
		t.Fatal("expected terminal reset to disable SGR mouse mode")
	}
}
