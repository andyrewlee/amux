package common

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// isAssistantsField reports whether item is the Assistants section, which
// (like the tmux fields) routes to a dedicated text-editing key handler
// rather than the generic list-navigation switch.
func isAssistantsField(item settingsItem) bool {
	return item == settingsItemAssistants
}

// SetAssistants sets the assistant roster the Assistants section lists: names
// in display order plus their current commands. Like SetUpdateInfo, this is
// populated after construction since the roster is late-bound app state (the
// caller reads it from config), not part of the dialog's core theme setup.
// Commands is copied so later edits to the dialog's in-memory copy cannot
// alias the caller's map.
func (s *SettingsDialog) SetAssistants(names []string, commands map[string]string) {
	s.assistantNames = names
	s.assistantCommands = make(map[string]string, len(commands))
	for name, cmd := range commands {
		s.assistantCommands[name] = cmd
	}
}

// AssistantCommands returns the (possibly edited) assistant command map so
// the caller can persist changes to config when the dialog closes.
func (s *SettingsDialog) AssistantCommands() map[string]string {
	return s.assistantCommands
}

// handleAssistantFieldKey edits the focused assistant's command field. It
// mirrors handleTmuxFieldKey's printable-keys-edit / structural-keys-move
// split, adapted for a dynamic list of rows living under a single
// settingsItem (assistantCursor selects the row, mirroring themeCursor):
// Tab/Shift+Tab and Enter leave the section like every other item, but
// Up/Down move the row cursor between assistants instead -- so, unlike the
// tmux fields, only the arrow keys are structural here; j/k and every other
// printable rune (including space) are typed into the focused command.
func (s *SettingsDialog) handleAssistantFieldKey(msg tea.KeyPressMsg) (*SettingsDialog, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "enter"))):
		return s.handleNextSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
		return s.handlePrevSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		s.moveAssistantCursor(1)
		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		s.moveAssistantCursor(-1)
		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		s.deleteFocusedAssistantRune()
		return s, nil
	}

	if msg.Text != "" {
		s.appendFocusedAssistantText(msg.Text)
	}
	return s, nil
}

// moveAssistantCursor moves assistantCursor by delta, wrapping within the
// roster (mirroring handleNext/handlePrev's theme-cursor wraparound).
func (s *SettingsDialog) moveAssistantCursor(delta int) {
	n := len(s.assistantNames)
	if n == 0 {
		return
	}
	s.assistantCursor = ((s.assistantCursor+delta)%n + n) % n
}

// focusedAssistantName returns the name at assistantCursor, or "" if the
// roster is empty or the cursor is out of range.
func (s *SettingsDialog) focusedAssistantName() (string, bool) {
	if s.assistantCursor < 0 || s.assistantCursor >= len(s.assistantNames) {
		return "", false
	}
	return s.assistantNames[s.assistantCursor], true
}

// appendFocusedAssistantText appends filtered text to the focused assistant's
// command, reusing the same printable-rune filter as the tmux fields.
func (s *SettingsDialog) appendFocusedAssistantText(txt string) {
	name, ok := s.focusedAssistantName()
	if !ok {
		return
	}
	s.assistantCommands[name] += keepRunes(txt, isPrintableFieldRune)
}

// deleteFocusedAssistantRune removes the last rune from the focused
// assistant's command.
func (s *SettingsDialog) deleteFocusedAssistantRune() {
	name, ok := s.focusedAssistantName()
	if !ok {
		return
	}
	s.assistantCommands[name] = trimLastRune(s.assistantCommands[name])
}
