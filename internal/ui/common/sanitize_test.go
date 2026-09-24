package common

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeDisplayText(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		maxRunes int
		want     string
	}{
		{"empty", "", 10, ""},
		{"plain", "hello world", 100, "hello world"},
		{"zero cap", "abc", 0, ""},
		// Well-formed sequences are removed whole (params included); malformed
		// or truncated ones lose their control bytes, leaving harmless text.
		{"esc stripped", "\x1b[2Jevil\x1b[0m", 100, "evil"},
		{"csi stripped", "a\x1b[31mb", 100, "ab"},
		{"osc52 stripped", "x\x1b]52;c;payload\ay", 100, "xy"},
		{"osc8 target removed", "evil\x1b]8;;https://example.com\aname.go", 100, "evilname.go"},
		{"truncated seq neutralized", "a\x1b[31", 100, "a"},
		{"c0 bytes", "a\x00b\x07c\x08d\x0ae", 100, "abcde"},
		{"del stripped", "a\x7fb", 100, "ab"},
		{"c1 runes", "a\u0085b\u009fc", 100, "abc"},
		{"c1 raw bytes", "a\x85b\x9fc", 100, "ab"},
		{"tab stripped", "a\tb", 100, "ab"},
		{"unicode kept", "héllo wörld ✓", 100, "héllo wörld ✓"},
		{"wide runes kept", "日本語テスト", 100, "日本語テスト"},
		{"invalid byte dropped", "a\xffb", 100, "ab"},
		{"cap honored", strings.Repeat("x", 20), 5, "xxxxx"},
		{"cap counts runes not bytes", "日本語テスト", 3, "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeDisplayText(tt.in, tt.maxRunes)
			if got != tt.want {
				t.Fatalf("SanitizeDisplayText(%q, %d) = %q, want %q", tt.in, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestSanitizeDisplayTextPreservesValidUTF8(t *testing.T) {
	in := "héllo — naïve — 日本語 — emoji 🎉"
	if got := SanitizeDisplayText(in, 1000); got != in {
		t.Fatalf("valid UTF-8 altered: %q", got)
	}
	if !utf8.ValidString(SanitizeDisplayText("ok\x1b[0m", 100)) {
		t.Fatal("result should be valid UTF-8 after stripping")
	}
}
