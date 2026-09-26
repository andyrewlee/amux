package common

import (
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
			got := pasteFirstLine(tc.input)
			if got != tc.want {
				t.Fatalf("pasteFirstLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("pasteFirstLine(%q) emitted invalid UTF-8 %q", tc.input, got)
			}
		})
	}

	t.Run("invalid UTF-8 yields replacement runes not garbage", func(t *testing.T) {
		got := pasteFirstLine("a\xffb")
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 emitted: %q", got)
		}
		// range decodes the bad byte as U+FFFD, which is graphic and kept.
		if got != "a\ufffdb" {
			t.Fatalf("pasteFirstLine invalid bytes = %q, want %q", got, "a\ufffdb")
		}
	})
}
