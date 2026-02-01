package vterm

import "testing"

func TestAltScreenScrollbackDisabled(t *testing.T) {
	vt := New(3, 2)
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatalf("expected AltScreen to be true")
	}

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected no scrollback in alt screen by default, got %d", len(vt.Scrollback))
	}
}

func TestAltScreenScrollbackEnabled(t *testing.T) {
	vt := New(3, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.AltScreen {
		t.Fatalf("expected AltScreen to be true")
	}

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) == 0 {
		t.Fatalf("expected scrollback in alt screen when enabled")
	}
}
