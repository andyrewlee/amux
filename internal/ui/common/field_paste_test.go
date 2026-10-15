package common

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFieldPasteFirstLinePolicy pins the shared single-line paste policy:
// only the first logical line is used, newline styles normalize, ordinary
// spaces survive, nongraphic controls are dropped, and the result is always
// valid UTF-8 for downstream field filters.
func TestFieldPasteFirstLinePolicy(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "make build", "make build"},
		{"LF takes first line", "first\nsecond\nthird", "first"},
		{"CRLF takes first line", "first\r\nsecond", "first"},
		{"lone CR takes first line", "first\rsecond", "first"},
		{"ordinary spaces preserved", "  spaced  \nnext", "  spaced  "},
		{"empty input", "", ""},
		{"empty first line", "\nsecond", ""},
		{"newline-only", "\n", ""},
		{"tabs dropped", "a\tb", "ab"},
		{"C0 and DEL dropped", "a\x07\x00\x1fb\x7f", "ab"},
		{"C1 controls dropped", "x\u0080\u009fy", "xy"},
		{"combining marks kept", "éclair", "éclair"},
		{"unicode text kept", "echo 你好", "echo 你好"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PasteFirstLine(tc.input)
			if got != tc.want {
				t.Fatalf("PasteFirstLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("PasteFirstLine(%q) emitted invalid UTF-8 %q", tc.input, got)
			}
		})
	}

	t.Run("invalid UTF-8 yields replacement runes not garbage", func(t *testing.T) {
		got := PasteFirstLine("a\xffb")
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 emitted: %q", got)
		}
		// range decodes the bad byte as U+FFFD, which is graphic and kept.
		if got != "a\ufffdb" {
			t.Fatalf("PasteFirstLine invalid bytes = %q, want %q", got, "a\ufffdb")
		}
	})
}

// TestSanitizePanePasteBreakout pins the exploit case: a clipboard payload
// carrying an embedded paste-end marker would end bracketed mode inside a
// 2004-honoring agent and turn the trailing bytes into keystrokes — the
// sanitizer must leave no ESC byte (so no marker can survive) while keeping
// the post-terminator text inside the paste as inert literal content.
func TestSanitizePanePasteBreakout(t *testing.T) {
	payload := "echo ok" + "\x1b[201~" + "\nrm -rf ~\r"
	out := SanitizePanePaste(payload)
	if strings.Contains(out, "\x1b") {
		t.Fatalf("ESC byte survived sanitization: %q", out)
	}
	if strings.Contains(out, "\x1b[201~") || strings.Contains(out, "\x1b[200~") {
		t.Fatalf("bracketed-paste marker survived: %q", out)
	}
	if !strings.Contains(out, "rm -rf ~") {
		t.Fatalf("post-terminator text should stay inside the paste, got %q", out)
	}
	// The wrapped payload must differ from the raw input — the red-check
	// for the call sites (raw msg.Content would carry the ESC through).
	if out == payload {
		t.Fatal("sanitizer was a no-op")
	}
}

// TestSanitizePanePastePolicy pins the byte policy: CRLF/lone-CR normalize
// to LF, tabs and newlines survive, every other C0/C1/DEL control rune is
// dropped, and printable UTF-8 (emoji incl. ZWJ sequences, CJK) passes
// through untouched.
func TestSanitizePanePastePolicy(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"crlf normalizes", "a\r\nb\rc", "a\nb\nc"},
		{"tab and newline kept", "x\ty\nz", "x\ty\nz"},
		{"c0 stripped", "a\x07b\x01c", "abc"},
		{"del stripped", "a\x7fb", "ab"},
		{"c1 stripped", "a\u0085b\u009bc", "abc"},
		{"utf8 kept", "héllo 世界 👨\u200d💻", "héllo 世界 👨\u200d💻"},
		{"empty", "", ""},
		{"only controls", "\x00\x1f\u009f", ""},
		{"invalid utf8 replaced", "a\xffb", "a�b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizePanePaste(tc.input); got != tc.want {
				t.Fatalf("SanitizePanePaste(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
