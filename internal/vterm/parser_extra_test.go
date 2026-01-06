package vterm

import "testing"

func TestParserLegacyAltScreen47(t *testing.T) {
	vt := New(80, 24)

	// Enable Alt Screen using CSI ? 47 h
	vt.Write([]byte("\x1b[?47h"))
	if !vt.AltScreen {
		t.Errorf("Expected AltScreen to be true after CSI ? 47 h")
	}

	// Disable Alt Screen using CSI ? 47 l
	vt.Write([]byte("\x1b[?47l"))
	if vt.AltScreen {
		t.Errorf("Expected AltScreen to be false after CSI ? 47 l")
	}
}

func TestParserLegacyAltScreen1047(t *testing.T) {
	vt := New(80, 24)

	// Enable Alt Screen using CSI ? 1047 h
	vt.Write([]byte("\x1b[?1047h"))
	if !vt.AltScreen {
		t.Errorf("Expected AltScreen to be true after CSI ? 1047 h")
	}

	// Disable Alt Screen using CSI ? 1047 l
	vt.Write([]byte("\x1b[?1047l"))
	if vt.AltScreen {
		t.Errorf("Expected AltScreen to be false after CSI ? 1047 l")
	}
}
