package app

import (
	"strings"
	"testing"
)

func TestSanitizedWindowTitle_StripsTerminalControls(t *testing.T) {
	title := "safe\x00\x1b[31m" + string(rune(0x9c)) + string([]byte{0x9b}) + "title"

	got := sanitizedWindowTitle(title)
	if got != "safe[31mtitle" {
		t.Fatalf("sanitizedWindowTitle() = %q, want %q", got, "safe[31mtitle")
	}
}

func TestSanitizedWindowTitle_CapsLength(t *testing.T) {
	got := sanitizedWindowTitle(strings.Repeat("x", maxWindowTitleRunes+10))

	if len(got) != maxWindowTitleRunes {
		t.Fatalf("sanitizedWindowTitle() length = %d, want %d", len(got), maxWindowTitleRunes)
	}
}
