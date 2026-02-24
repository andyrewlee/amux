package center

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

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

func recordLocalInputEchoWindow(tab *Tab, data string, now time.Time) {
	if tab == nil {
		return
	}
	tab.mu.Lock()
	tab.bootstrapActivity = false
	tab.bootstrapLastOutputAt = time.Time{}
	if shouldSuppressLocalEchoInput(data) {
		tab.lastUserInputAt = now
	} else {
		tab.lastUserInputAt = time.Time{}
	}
	tab.mu.Unlock()
}
