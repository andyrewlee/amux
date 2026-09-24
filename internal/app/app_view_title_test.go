package app

import (
	"strings"
	"testing"
)

func TestSanitizedWindowTitle_StripsTerminalControls(t *testing.T) {
	title := "safe\x00\x1b[31m" + string(rune(0x9c)) + string([]byte{0x9b}) + "title"

	// ansi.Strip consumes whole sequences including parameter bytes, and the
	// C1 CSI opener 0x9b takes "t" as its final byte — so "itle" survives.
	got := sanitizedWindowTitle(title)
	if got != "safeitle" {
		t.Fatalf("sanitizedWindowTitle() = %q, want %q", got, "safeitle")
	}
}

func TestSanitizedWindowTitle_CapsLength(t *testing.T) {
	got := sanitizedWindowTitle(strings.Repeat("x", maxWindowTitleRunes+10))

	if len(got) != maxWindowTitleRunes {
		t.Fatalf("sanitizedWindowTitle() length = %d, want %d", len(got), maxWindowTitleRunes)
	}
}

func TestFocusedWindowTitle_FallsBackWhenEmpty(t *testing.T) {
	tests := []struct {
		name  string
		title string
	}{
		{name: "empty", title: ""},
		{name: "controls only", title: "\x00\x1b" + string(rune(0x9c))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := focusedWindowTitle(tt.title)
			if got != fallbackWindowTitle {
				t.Fatalf("focusedWindowTitle(%q) = %q, want %q", tt.title, got, fallbackWindowTitle)
			}
		})
	}
}
