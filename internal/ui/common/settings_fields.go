package common

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// handleTextFieldKey edits the focused text field. Structural keys move
// between fields/sections; backspace deletes; any other printable text is
// appended (filtered to valid characters for the field).
func (s *SettingsDialog) handleTextFieldKey(msg tea.KeyPressMsg) (*SettingsDialog, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down", "enter"))):
		return s.handleNextSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
		return s.handlePrevSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		s.deleteFocusedTextRune()
		return s, nil
	}

	if msg.Text != "" {
		s.appendFocusedText(msg.Text)
	}
	return s, nil
}

// appendFocusedText appends filtered text to the focused text field. The
// sync-interval field only accepts characters that can appear in a Go
// duration so the UI cannot persist a value the consumer would reject and
// silently replace with its default; the viewer command is a shell fragment
// and takes any printable rune (validated at open time, not here).
func (s *SettingsDialog) appendFocusedText(txt string) {
	switch s.focusedItem {
	case settingsItemTmuxServer:
		s.tmuxServer += keepRunes(txt, isPrintableFieldRune)
	case settingsItemTmuxConfig:
		s.tmuxConfigPath += keepRunes(txt, isPrintableFieldRune)
	case settingsItemTmuxSync:
		s.tmuxSyncInterval += keepRunes(txt, isDurationRune)
	case settingsItemViewerCmd:
		s.viewerCmd += keepRunes(txt, isPrintableFieldRune)
	}
}

// deleteFocusedTextRune removes the last rune from the focused text field.
func (s *SettingsDialog) deleteFocusedTextRune() {
	switch s.focusedItem {
	case settingsItemTmuxServer:
		s.tmuxServer = TrimLastRune(s.tmuxServer)
	case settingsItemTmuxConfig:
		s.tmuxConfigPath = TrimLastRune(s.tmuxConfigPath)
	case settingsItemTmuxSync:
		s.tmuxSyncInterval = TrimLastRune(s.tmuxSyncInterval)
	case settingsItemViewerCmd:
		s.viewerCmd = TrimLastRune(s.viewerCmd)
	}
}

func keepRunes(s string, keep func(rune) bool) string {
	var b strings.Builder
	for _, r := range s {
		if keep(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// KeepPrintable drops every nongraphic rune from s — the plain-text filter the
// single-line fields and search queries share.
func KeepPrintable(s string) string {
	return keepRunes(s, isPrintableFieldRune)
}

// TrimLastRune drops the last rune of s (backspace at the end of a field).
func TrimLastRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}

// isPrintableFieldRune accepts any printable rune (spaces included, so paths and
// server names with spaces work) while rejecting control characters.
func isPrintableFieldRune(r rune) bool {
	return unicode.IsGraphic(r)
}

// isDurationRune accepts only characters that can appear in a Go duration string
// (time.ParseDuration): digits, a decimal point, and lowercase unit letters
// covering ns, us/µs, ms, s, m, and h.
func isDurationRune(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune(".nsuµmh", r)
}
