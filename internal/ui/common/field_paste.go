package common

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// PasteFirstLine normalizes a bracketed-paste payload for the custom
// single-line editors (env vars, lifecycle scripts, assistant commands, tmux
// settings, search queries). CRLF and lone CR collapse to LF, only the first
// logical line is kept (these are single-line fields — a pasted newline must
// never act as Enter), and nongraphic control runes are dropped. Ordinary
// leading/trailing spaces survive: they are legitimate content in commands
// and values.
//
// The destination field's own filter still runs on the result (printable for
// text fields, isDurationRune for the tmux sync interval), so this helper only
// owns the line/control policy shared by every paste site.
func PasteFirstLine(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	line, _, _ := strings.Cut(content, "\n")
	return keepRunes(line, isPrintableFieldRune)
}

// SanitizePanePaste normalizes a bracketed-paste payload bound for a
// terminal pane's PTY. Unlike PasteFirstLine the pane keeps the payload
// multi-line — newlines and tabs survive — but every other control rune is
// dropped: an embedded \x1b[201~ would end bracketed mode early inside any
// 2004-honoring agent and turn the remaining bytes into live keystrokes
// (the classic pastejack), and stray C0/C1/DEL bytes otherwise reach the
// pty untouched. The literal marker strips are a defense-in-depth
// invariant, not a second mechanism — the ESC byte is a control rune and
// cannot survive the filter, so the marker sequences are already gone by
// the time we look for them.
func SanitizePanePaste(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = keepRunes(content, isPanePasteRune)
	content = strings.ReplaceAll(content, ansi.BracketedPasteStart, "")
	content = strings.ReplaceAll(content, ansi.BracketedPasteEnd, "")
	return content
}

// isPanePasteRune keeps any non-control rune plus newline and tab —
// printable text, combining marks, emoji joiners, and private-use
// characters all pass through unchanged for the agent's own terminal to
// render. Invalid UTF-8 decodes as U+FFFD (a graphic rune), so undecodable
// bytes land as replacement characters rather than raw escape fodder.
func isPanePasteRune(r rune) bool {
	return r == '\n' || r == '\t' || !unicode.IsControl(r)
}
