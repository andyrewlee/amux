package center

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func isDigitsAndSemicolons(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != ';' {
			return false
		}
	}
	return true
}

// isLocalEditingEscapeSequence reports escape sequences commonly produced by
// navigation/editing keys (arrow keys, home/end, insert/delete, etc.).
func isLocalEditingEscapeSequence(data string) bool {
	if len(data) < 2 || data[0] != '\x1b' {
		return false
	}
	switch data[1] {
	case '[':
		if len(data) < 3 {
			return false
		}
		final := data[len(data)-1]
		params := data[2 : len(data)-1]
		switch final {
		case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H':
			return isDigitsAndSemicolons(params)
		case 'Z':
			return params == ""
		case '~':
			if !isDigitsAndSemicolons(params) {
				return false
			}
			first := params
			if idx := strings.IndexByte(first, ';'); idx >= 0 {
				first = first[:idx]
			}
			switch first {
			case "1", "2", "3", "4", "7", "8":
				return true
			default:
				return false
			}
		default:
			return false
		}
	case 'O':
		if len(data) != 3 {
			return false
		}
		switch data[2] {
		case 'A', 'B', 'C', 'D', 'F', 'H':
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// isReadlineEditingControlInput reports single-byte control characters commonly
// used for prompt editing/navigation in readline-style CLIs.
func isReadlineEditingControlInput(data string) bool {
	if len(data) != 1 {
		return false
	}
	switch data[0] {
	case 0x01, // Ctrl-A: start of line
		0x02, // Ctrl-B: backward char
		0x05, // Ctrl-E: end of line
		0x06, // Ctrl-F: forward char
		0x08, // Ctrl-H: backward delete
		0x0b, // Ctrl-K: kill to end
		0x0c, // Ctrl-L: clear/redraw
		0x0e, // Ctrl-N: next history
		0x10, // Ctrl-P: previous history
		0x14, // Ctrl-T: transpose chars
		0x15, // Ctrl-U: kill to start
		0x17, // Ctrl-W: backward kill word
		0x19: // Ctrl-Y: yank
		return true
	default:
		return false
	}
}

// isReadlinePromptControlInput reports control bytes that typically trigger a
// prompt redraw/reset rather than normal text editing.
func isReadlinePromptControlInput(data string) bool {
	if len(data) != 1 {
		return false
	}
	switch data[0] {
	case 0x03, // Ctrl-C: interrupt/reset prompt
		0x07: // Ctrl-G: abort completion/search
		return true
	default:
		return false
	}
}

// isReadlineEditingEscapeInput reports ESC-prefixed prompt-editing input such
// as bare Escape and Alt+<key> sequences that readline-style prompts use for
// word motions and mode changes.
func isReadlineEditingEscapeInput(data string) bool {
	if data == "\x1b" {
		return true
	}
	if len(data) < 2 || data[0] != '\x1b' {
		return false
	}
	if data[1] == '[' || data[1] == 'O' || data[1] == ']' {
		return false
	}
	_, size := utf8.DecodeRuneInString(data[1:])
	return size == len(data)-1 && size > 0
}

func bracketedPasteContent(data string) (string, bool) {
	const (
		pasteStart = "\x1b[200~"
		pasteEnd   = "\x1b[201~"
	)
	if !strings.HasPrefix(data, pasteStart) || !strings.HasSuffix(data, pasteEnd) {
		return "", false
	}
	return data[len(pasteStart) : len(data)-len(pasteEnd)], true
}

// shouldSuppressPromptEchoInput reports prompt-only edits that should reuse the
// local-echo suppression path so their eventual PTY redraws are not
// misclassified as assistant output. This intentionally excludes bracketed
// paste that ends with a submit newline/carriage return.
func shouldSuppressPromptEchoInput(data string) bool {
	if isReadlineEditingEscapeInput(data) {
		return true
	}
	content, ok := bracketedPasteContent(data)
	if !ok {
		return false
	}
	return !strings.HasSuffix(content, "\r") && !strings.HasSuffix(content, "\n")
}

func shouldSuppressLocalEchoInput(data string) bool {
	if data == "" {
		return false
	}
	if strings.HasPrefix(data, "\x1b[200~") {
		return false
	}
	if data == "\r" || data == "\n" {
		return false
	}
	if isReadlineEditingControlInput(data) {
		return true
	}
	if isLocalEditingEscapeSequence(data) {
		return true
	}
	if strings.HasPrefix(data, "\x1b") {
		return false
	}
	// Backspace and tab are local editing/typing-like input.
	if data == "\x7f" || data == "\t" {
		return true
	}
	// Single printable rune (including UTF-8, and space) is typing-like.
	r, size := utf8.DecodeRuneInString(data)
	if r == utf8.RuneError && size == 1 {
		return false
	}
	if size != len(data) {
		return false
	}
	return unicode.IsPrint(r)
}

func shouldTrackRecentChatPromptInput(data string) bool {
	if data == "" {
		return false
	}
	return shouldSuppressLocalEchoInput(data) ||
		isReadlinePromptControlInput(data) ||
		data == "\r" || data == "\n" ||
		isReadlineEditingEscapeInput(data) ||
		strings.HasPrefix(data, "\x1b[200~")
}

func isSubmitStylePromptInput(data string) bool {
	if data == "\r" || data == "\n" || isReadlinePromptControlInput(data) {
		return true
	}
	content, ok := bracketedPasteContent(data)
	if !ok {
		return false
	}
	return strings.HasSuffix(content, "\r") || strings.HasSuffix(content, "\n")
}

func submittedBracketedPasteEchoContent(data string) string {
	content, ok := bracketedPasteContent(data)
	if !ok {
		return ""
	}
	content = strings.TrimRight(content, "\r\n")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "")
	if content == "" || !isSubmitStylePromptInput(data) {
		return ""
	}
	return content
}

func recordLocalInputEchoWindow(tab *Tab, data string, now time.Time) {
	if tab == nil {
		return
	}
	tab.mu.Lock()
	tab.bootstrapActivity = false
	tab.bootstrapLastOutputAt = time.Time{}
	tab.pendingSubmitPasteEcho = submittedBracketedPasteEchoContent(data)
	if shouldSuppressLocalEchoInput(data) || shouldSuppressPromptEchoInput(data) {
		tab.lastUserInputAt = now
	} else {
		tab.lastUserInputAt = time.Time{}
	}
	if shouldTrackRecentChatPromptInput(data) {
		tab.lastPromptInputAt = now
	} else {
		tab.lastPromptInputAt = time.Time{}
	}
	if isSubmitStylePromptInput(data) {
		tab.lastPromptSubmitAt = now
	} else {
		tab.lastPromptSubmitAt = time.Time{}
	}
	tab.mu.Unlock()
}
