package common

import "strings"

// pasteFirstLine normalizes a bracketed-paste payload for the custom
// single-line editors (env vars, lifecycle scripts, assistant commands, tmux
// settings). CRLF and lone CR collapse to LF, only the first logical line is
// kept (these are single-line fields — a pasted newline must never act as
// Enter), and nongraphic control runes are dropped. Ordinary leading/trailing
// spaces survive: they are legitimate content in commands and values.
//
// The destination field's own filter still runs on the result (printable for
// text fields, isDurationRune for the tmux sync interval), so this helper only
// owns the line/control policy shared by every paste site.
func pasteFirstLine(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	line, _, _ := strings.Cut(content, "\n")
	return keepRunes(line, isPrintableFieldRune)
}
