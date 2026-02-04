package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/messages"
)

// PermissionsEditor is a modal for editing the global allow/deny lists.
type PermissionsEditor struct {
	visible   bool
	width     int
	height    int
	allowList []string
	denyList  []string

	activeList int // 0=allow, 1=deny
	cursor     int
	addingNew  bool
	editing    bool
	editIndex  int
	input      textinput.Model
}

// NewPermissionsEditor creates a new permissions editor.
func NewPermissionsEditor(allow, deny []string) *PermissionsEditor {
	// Deduplicate the lists
	allowCopy := dedupeStrings(allow)
	denyCopy := dedupeStrings(deny)

	input := textinput.New()
	input.Placeholder = "Bash(npm:*)"
	input.SetWidth(30)
	input.SetVirtualCursor(true)

	return &PermissionsEditor{
		allowList: allowCopy,
		denyList:  denyCopy,
		input:     input,
	}
}

// dedupeStrings removes duplicates from a string slice while preserving order.
func dedupeStrings(perms []string) []string {
	if perms == nil {
		return []string{}
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(perms))
	for _, p := range perms {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}
	return result
}

func (e *PermissionsEditor) Show()            { e.visible = true }
func (e *PermissionsEditor) Hide()            { e.visible = false }
func (e *PermissionsEditor) Visible() bool    { return e.visible }
func (e *PermissionsEditor) SetSize(w, h int) { e.width, e.height = w, h }

func (e *PermissionsEditor) activeEntries() []string {
	if e.activeList == 0 {
		return e.allowList
	}
	return e.denyList
}

func (e *PermissionsEditor) setActiveEntries(entries []string) {
	if e.activeList == 0 {
		e.allowList = entries
	} else {
		e.denyList = entries
	}
}

func (e *PermissionsEditor) setActiveEntry(index int, value string) {
	if e.activeList == 0 {
		if index >= 0 && index < len(e.allowList) {
			e.allowList[index] = value
		}
	} else {
		if index >= 0 && index < len(e.denyList) {
			e.denyList[index] = value
		}
	}
}

// Update handles input for the permissions editor.
func (e *PermissionsEditor) Update(msg tea.Msg) (*PermissionsEditor, tea.Cmd) {
	if !e.visible {
		return e, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if e.addingNew || e.editing {
			return e.handleInputMode(msg)
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			e.visible = false
			return e, func() tea.Msg {
				return messages.PermissionsEditorResult{Confirmed: false}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "h", "l", "left", "right"))):
			e.activeList = (e.activeList + 1) % 2
			entries := e.activeEntries()
			if e.cursor >= len(entries) {
				e.cursor = max(0, len(entries)-1)
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			entries := e.activeEntries()
			if e.cursor < len(entries)-1 {
				e.cursor++
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if e.cursor > 0 {
				e.cursor--
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			e.addingNew = true
			e.editing = false
			e.input.SetValue("")
			e.input.Focus()
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("e", "enter"))):
			entries := e.activeEntries()
			if len(entries) > 0 && e.cursor < len(entries) {
				e.editing = true
				e.addingNew = false
				e.editIndex = e.cursor
				e.input.SetValue(entries[e.cursor])
				e.input.Focus()
				e.input.SetCursor(len(entries[e.cursor]))
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("d", "delete", "backspace"))):
			entries := e.activeEntries()
			if len(entries) > 0 && e.cursor < len(entries) {
				entries = append(entries[:e.cursor], entries[e.cursor+1:]...)
				e.setActiveEntries(entries)
				if e.cursor >= len(entries) && e.cursor > 0 {
					e.cursor--
				}
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("m"))):
			entries := e.activeEntries()
			if len(entries) > 0 && e.cursor < len(entries) {
				perm := entries[e.cursor]
				// Remove from current list
				entries = append(entries[:e.cursor], entries[e.cursor+1:]...)
				e.setActiveEntries(entries)
				// Add to other list
				if e.activeList == 0 {
					e.denyList = append(e.denyList, perm)
				} else {
					e.allowList = append(e.allowList, perm)
				}
				if e.cursor >= len(e.activeEntries()) && e.cursor > 0 {
					e.cursor--
				}
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("S"))):
			e.visible = false
			return e, func() tea.Msg {
				return messages.PermissionsEditorResult{
					Confirmed: true,
					Allow:     e.allowList,
					Deny:      e.denyList,
				}
			}
		}

	case tea.PasteMsg:
		if e.addingNew || e.editing {
			var cmd tea.Cmd
			e.input, cmd = e.input.Update(msg)
			return e, cmd
		}
	}

	return e, nil
}

func (e *PermissionsEditor) handleInputMode(msg tea.KeyPressMsg) (*PermissionsEditor, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		value := e.input.Value()
		if value != "" {
			if e.editing {
				e.setActiveEntry(e.editIndex, value)
			} else {
				entries := e.activeEntries()
				entries = append(entries, value)
				e.setActiveEntries(entries)
				e.cursor = len(e.activeEntries()) - 1
			}
		}
		e.addingNew = false
		e.editing = false
		e.input.Blur()
		return e, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		e.addingNew = false
		e.editing = false
		e.input.Blur()
		return e, nil
	}

	var cmd tea.Cmd
	e.input, cmd = e.input.Update(msg)
	return e, cmd
}
