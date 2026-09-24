package common

import (
	"strings"

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
	s.assistantsSet = true
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

// SetAssistantNameValidator installs the optional domain check run on a new
// assistant name when the user commits an add ("" return = accept; anything
// else renders as the in-dialog error). The app wires the same rules the
// config loader applies (validation.ValidateAssistant) so a name added here
// survives a restart instead of being dropped at load.
func (s *SettingsDialog) SetAssistantNameValidator(fn func(string) string) {
	s.assistantNameValidator = fn
}

// handleAssistantFieldKey edits the focused assistant's command field. It
// mirrors handleTmuxFieldKey's printable-keys-edit / structural-keys-move
// split, adapted for a dynamic list of rows living under a single
// settingsItem (assistantCursor selects the row, mirroring themeCursor):
// Tab/Shift+Tab and Enter leave the section like every other item, but
// Up/Down move the row cursor between assistants instead -- so, unlike the
// tmux fields, only the arrow keys are structural here; j/k and every other
// printable rune (including space) are typed into the focused command.
// Ctrl+A opens the two-field add input, which captures keys until committed
// or canceled.
func (s *SettingsDialog) handleAssistantFieldKey(msg tea.KeyPressMsg) (*SettingsDialog, tea.Cmd) {
	s.assistantNotice = ""

	if s.assistantAdding {
		return s.updateAssistantAddMode(msg)
	}

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

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+a"))):
		s.assistantAdding = true
		s.assistantAddField = 0
		s.assistantAddName = ""
		s.assistantAddCmd = ""
		s.assistantAddError = ""
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

// updateAssistantAddMode handles input while the assistant add input is
// open: Tab toggles between name and command, Enter advances name->command
// (when the name is valid) and commits from the command field, Esc cancels
// just the add. Structural keys stay inert so the section can't be left
// mid-add (Tab is the field toggle here, not the section-move it is in list
// mode).
func (s *SettingsDialog) updateAssistantAddMode(msg tea.KeyPressMsg) (*SettingsDialog, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		s.assistantAdding = false
		s.assistantNotice = "add canceled"
		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "shift+tab"))):
		s.assistantAddField = 1 - s.assistantAddField
		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if s.assistantAddField == 0 {
			if err := s.validateAssistantAddName(); err != "" {
				s.assistantAddError = err
				return s, nil
			}
			s.assistantAddError = ""
			s.assistantAddField = 1
			return s, nil
		}
		s.commitAssistantAdd()
		return s, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		if s.assistantAddField == 0 {
			s.assistantAddName = trimLastRune(s.assistantAddName)
		} else {
			s.assistantAddCmd = trimLastRune(s.assistantAddCmd)
		}
		return s, nil
	}

	if msg.Text != "" {
		txt := keepRunes(msg.Text, isPrintableFieldRune)
		if s.assistantAddField == 0 {
			s.assistantAddName += txt
		} else {
			s.assistantAddCmd += txt
		}
	}
	return s, nil
}

// validateAssistantAddName enforces the generic name rules for a new
// assistant (non-empty, printable, no whitespace) plus the optional domain
// validator from SetAssistantNameValidator. Names normalize
// (trim+lowercase) the way config lookups do so "Claude" can't shadow
// "claude".
func (s *SettingsDialog) validateAssistantAddName() string {
	name := normalizeAssistantNameInput(s.assistantAddName)
	if name == "" {
		return "name required"
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return "name must not contain whitespace"
	}
	for _, r := range name {
		if !isPrintableFieldRune(r) {
			return "name must be printable"
		}
	}
	if s.assistantNameValidator != nil {
		if err := s.assistantNameValidator(name); err != "" {
			return err
		}
	}
	return ""
}

// normalizeAssistantNameInput applies the same normalization config lookups
// use (lowercase + trim) so add-time dedupe and later IsAssistantKnown agree.
func normalizeAssistantNameInput(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// commitAssistantAdd applies the add: a duplicate name cancels the add and
// focuses the existing row (never silently overwrite); a new name appends
// to the roster end (config re-sorts extras on load) with its command.
func (s *SettingsDialog) commitAssistantAdd() {
	name := normalizeAssistantNameInput(s.assistantAddName)
	if err := s.validateAssistantAddName(); err != "" {
		s.assistantAddError = err
		return
	}
	if strings.TrimSpace(s.assistantAddCmd) == "" {
		s.assistantAddError = "command required"
		return
	}
	if _, exists := s.assistantCommands[name]; exists {
		for i, n := range s.assistantNames {
			if n == name {
				s.assistantCursor = i
				break
			}
		}
		s.assistantAdding = false
		s.assistantNotice = name + " already exists"
		return
	}
	if s.assistantCommands == nil {
		s.assistantCommands = make(map[string]string)
	}
	s.assistantNames = append(s.assistantNames, name)
	s.assistantCommands[name] = s.assistantAddCmd
	s.assistantCursor = len(s.assistantNames) - 1
	s.assistantAdding = false
	s.assistantNotice = "added " + name
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
