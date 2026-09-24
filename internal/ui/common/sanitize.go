package common

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// SanitizeDisplayText returns s safe to render into a frame when a repository
// or filesystem entry can influence it: well-formed ANSI escape sequences are
// removed whole (ansi.Strip — so OSC hyperlink targets don't survive as
// literal text), then any remaining terminal control bytes/runes — C0
// controls, DEL, and C1 controls (0x80–0x9f), including a truncated or
// malformed sequence's stray bytes — are dropped, capped at maxRunes runes.
// The result is single-line — callers composing multi-line text should
// sanitize each piece before joining. Logs and PTY output stay raw by design.
func SanitizeDisplayText(s string, maxRunes int) string {
	if s == "" || maxRunes <= 0 {
		return ""
	}
	s = ansi.Strip(s)
	var b strings.Builder
	written := 0
	for len(s) > 0 && written < maxRunes {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			raw := s[0]
			s = s[1:]
			if isTerminalControlByte(raw) {
				continue
			}
		} else {
			s = s[size:]
		}
		if isTerminalControlRune(r) {
			continue
		}
		if b.Len() == 0 {
			b.Grow(len(s))
		}
		b.WriteRune(r)
		written++
	}
	return b.String()
}

// sanitizeDisplayLines is the multi-line sibling of SanitizeDisplayText: it
// splits on '\n', sanitizes each line, and rejoins — the line structure the
// caller intends is preserved, but no line can carry escapes, control bytes,
// or a fabricated newline of its own.
func sanitizeDisplayLines(s string, maxRunesPerLine int) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = SanitizeDisplayText(p, maxRunesPerLine)
	}
	return strings.Join(parts, "\n")
}

func isTerminalControlByte(b byte) bool {
	return b <= 0x1f || b == 0x7f || (b >= 0x80 && b <= 0x9f)
}

func isTerminalControlRune(r rune) bool {
	return r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
