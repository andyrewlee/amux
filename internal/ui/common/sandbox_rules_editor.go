package common

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/messages"
)

var sandboxActions = []config.SandboxAction{
	config.SandboxAllowWrite,
	config.SandboxDenyRead,
	config.SandboxAllowRead,
}

var sandboxPathTypes = []config.SandboxPathType{
	config.SandboxSubpath,
	config.SandboxLiteral,
	config.SandboxRegex,
}

// SandboxRulesEditor is a modal for editing sandbox path rules.
type SandboxRulesEditor struct {
	visible      bool
	width        int
	height       int
	rules        []config.SandboxRule
	cursor       int
	scrollOffset int
	addingNew    bool
	editing      bool
	editIndex    int
	pathInput    textinput.Model
	actionIdx    int // index into sandboxActions
	pathTypeIdx  int // index into sandboxPathTypes
}

// NewSandboxRulesEditor creates a new sandbox rules editor.
func NewSandboxRulesEditor(rules []config.SandboxRule) *SandboxRulesEditor {
	rulesCopy := make([]config.SandboxRule, len(rules))
	copy(rulesCopy, rules)

	input := textinput.New()
	input.Placeholder = "~/.example"
	input.SetWidth(40)
	input.SetVirtualCursor(true)

	return &SandboxRulesEditor{
		rules:     rulesCopy,
		pathInput: input,
	}
}

func (e *SandboxRulesEditor) Show()            { e.visible = true }
func (e *SandboxRulesEditor) Hide()            { e.visible = false }
func (e *SandboxRulesEditor) Visible() bool    { return e.visible }
func (e *SandboxRulesEditor) SetSize(w, h int) { e.width, e.height = w, h }

// maxVisibleRows returns how many list rows can fit given the current height.
func (e *SandboxRulesEditor) maxVisibleRows() int {
	const maxRows = 20
	const fixedChrome = 14
	if e.height <= 0 {
		return maxRows
	}
	rows := e.height - fixedChrome
	if rows < 5 {
		rows = 5
	}
	if rows > maxRows {
		rows = maxRows
	}
	return rows
}

// ensureVisible adjusts scrollOffset so the cursor stays in the visible window.
func (e *SandboxRulesEditor) ensureVisible() {
	if len(e.rules) == 0 {
		e.scrollOffset = 0
		return
	}
	maxRows := e.maxVisibleRows()
	if e.cursor < e.scrollOffset {
		e.scrollOffset = e.cursor
	} else if e.cursor >= e.scrollOffset+maxRows {
		e.scrollOffset = e.cursor - maxRows + 1
	}
}

// Update handles input for the sandbox rules editor.
func (e *SandboxRulesEditor) Update(msg tea.Msg) (*SandboxRulesEditor, tea.Cmd) {
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
				return messages.SandboxRulesEditorResult{Confirmed: false}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			if e.cursor < len(e.rules)-1 {
				e.cursor++
			}
			e.ensureVisible()
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if e.cursor > 0 {
				e.cursor--
			}
			e.ensureVisible()
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			e.addingNew = true
			e.editing = false
			e.actionIdx = 0
			e.pathTypeIdx = 0
			e.pathInput.SetValue("")
			e.pathInput.Focus()
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("e", "enter"))):
			if len(e.rules) > 0 && e.cursor < len(e.rules) {
				rule := e.rules[e.cursor]
				if rule.Locked {
					return e, nil // cannot edit locked rules
				}
				e.editing = true
				e.addingNew = false
				e.editIndex = e.cursor
				e.pathInput.SetValue(rule.Path)
				e.pathInput.Focus()
				e.pathInput.SetCursor(len(rule.Path))
				// Set action and pathType indices
				e.actionIdx = actionIndex(rule.Action)
				e.pathTypeIdx = pathTypeIndex(rule.PathType)
			}
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("d", "delete", "backspace"))):
			if len(e.rules) > 0 && e.cursor < len(e.rules) {
				if e.rules[e.cursor].Locked {
					return e, nil // cannot delete locked rules
				}
				e.rules = append(e.rules[:e.cursor], e.rules[e.cursor+1:]...)
				if e.cursor >= len(e.rules) && e.cursor > 0 {
					e.cursor--
				}
			}
			e.ensureVisible()
			return e, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("S"))):
			e.visible = false
			return e, func() tea.Msg {
				return messages.SandboxRulesEditorResult{
					Confirmed: true,
					Rules:     e.rules,
				}
			}
		}

	case tea.PasteMsg:
		if e.addingNew || e.editing {
			var cmd tea.Cmd
			e.pathInput, cmd = e.pathInput.Update(msg)
			return e, cmd
		}
	}

	return e, nil
}

func (e *SandboxRulesEditor) handleInputMode(msg tea.KeyPressMsg) (*SandboxRulesEditor, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		path := e.pathInput.Value()
		if path != "" {
			action := sandboxActions[e.actionIdx]
			pathType := sandboxPathTypes[e.pathTypeIdx]
			if e.editing {
				e.rules[e.editIndex].Path = path
				e.rules[e.editIndex].Action = action
				e.rules[e.editIndex].PathType = pathType
			} else {
				e.rules = append(e.rules, config.SandboxRule{
					Path:     path,
					Action:   action,
					PathType: pathType,
				})
				e.cursor = len(e.rules) - 1
			}
		}
		e.addingNew = false
		e.editing = false
		e.pathInput.Blur()
		e.ensureVisible()
		return e, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		e.addingNew = false
		e.editing = false
		e.pathInput.Blur()
		return e, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
		e.actionIdx = (e.actionIdx + 1) % len(sandboxActions)
		return e, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
		e.pathTypeIdx = (e.pathTypeIdx + 1) % len(sandboxPathTypes)
		return e, nil
	}

	var cmd tea.Cmd
	e.pathInput, cmd = e.pathInput.Update(msg)
	return e, cmd
}

func actionIndex(a config.SandboxAction) int {
	for i, v := range sandboxActions {
		if v == a {
			return i
		}
	}
	return 0
}

func pathTypeIndex(pt config.SandboxPathType) int {
	for i, v := range sandboxPathTypes {
		if v == pt {
			return i
		}
	}
	return 0
}
