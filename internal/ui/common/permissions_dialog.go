package common

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/messages"
)

// PendingPermission represents a permission detected from a workspace.
type PendingPermission struct {
	Permission string
	Source     string
	Action     messages.PermissionActionType
}

// PermissionsDialog is a modal for reviewing pending permissions.
type PermissionsDialog struct {
	visible     bool
	width       int
	height      int
	permissions []PendingPermission
	cursor      int

	// Editing state
	editing   bool
	editIndex int
	input     textinput.Model
}

// NewPermissionsDialog creates a new permissions dialog.
func NewPermissionsDialog(pending []PendingPermission) *PermissionsDialog {
	// Copy and set default action to Allow
	perms := make([]PendingPermission, len(pending))
	for i, p := range pending {
		perms[i] = PendingPermission{
			Permission: p.Permission,
			Source:     p.Source,
			Action:     messages.PermissionAllow,
		}
	}

	input := textinput.New()
	input.Placeholder = "Bash(command *)"
	input.SetWidth(40)
	input.SetVirtualCursor(true)

	return &PermissionsDialog{
		permissions: perms,
		input:       input,
	}
}

func (d *PermissionsDialog) Show()            { d.visible = true }
func (d *PermissionsDialog) Hide()            { d.visible = false }
func (d *PermissionsDialog) Visible() bool    { return d.visible }
func (d *PermissionsDialog) Editing() bool    { return d.editing }
func (d *PermissionsDialog) SetSize(w, h int) { d.width, d.height = w, h }

// Update handles input for the permissions dialog.
func (d *PermissionsDialog) Update(msg tea.Msg) (*PermissionsDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Handle editing mode separately
		if d.editing {
			return d.handleEditMode(msg)
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			d.visible = false
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			if d.cursor < len(d.permissions) {
				d.cursor++
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			if d.cursor > 0 {
				d.cursor--
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			if d.cursor < len(d.permissions) {
				d.permissions[d.cursor].Action = messages.PermissionAllow
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("d"))):
			if d.cursor < len(d.permissions) {
				d.permissions[d.cursor].Action = messages.PermissionDeny
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
			if d.cursor < len(d.permissions) {
				d.permissions[d.cursor].Action = messages.PermissionSkip
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
			// Enter edit mode for current permission
			if d.cursor < len(d.permissions) {
				d.editing = true
				d.editIndex = d.cursor
				d.input.SetValue(d.permissions[d.cursor].Permission)
				d.input.SetCursor(len(d.permissions[d.cursor].Permission))
				d.input.Focus()
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right", " "))):
			if d.cursor < len(d.permissions) {
				d.permissions[d.cursor].Action = (d.permissions[d.cursor].Action + 1) % 3
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			if d.cursor < len(d.permissions) {
				d.permissions[d.cursor].Action = (d.permissions[d.cursor].Action + 2) % 3
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			// If cursor is on the Apply button row
			if d.cursor >= len(d.permissions) {
				return d, d.apply()
			}
			// On a permission row: cycle action
			d.permissions[d.cursor].Action = (d.permissions[d.cursor].Action + 1) % 3
			return d, nil
		}

	case tea.PasteMsg:
		if d.editing {
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			return d, cmd
		}
	}

	return d, nil
}

// handleEditMode handles key presses while editing a permission.
func (d *PermissionsDialog) handleEditMode(msg tea.KeyPressMsg) (*PermissionsDialog, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		// Save edited value
		value := d.input.Value()
		if value != "" && d.editIndex < len(d.permissions) {
			d.permissions[d.editIndex].Permission = value
		}
		d.editing = false
		d.input.Blur()
		return d, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		// Cancel edit
		d.editing = false
		d.input.Blur()
		return d, nil
	}

	// Pass other keys to the text input
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d *PermissionsDialog) apply() tea.Cmd {
	d.visible = false
	var actions []messages.PermissionAction
	for _, p := range d.permissions {
		actions = append(actions, messages.PermissionAction{
			Permission: p.Permission,
			Action:     p.Action,
		})
	}
	return func() tea.Msg {
		return messages.PermissionsDialogResult{Actions: actions}
	}
}
