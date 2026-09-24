package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ScriptsDialogResult is sent when the workspace scripts dialog closes.
// Canceled is true when the user dismissed via Esc, in which case the caller
// must discard every edit -- the same cancel contract EnvDialogResult uses.
type ScriptsDialogResult struct {
	Canceled bool
}

// ScriptsDialogMode values mirror data.ScriptMode ("nonconcurrent" default).
// The dialog is domain-agnostic (internal/ui/common imports neither
// internal/data nor internal/process): it edits four plain strings and the
// caller maps them back onto ws.Scripts/ws.ScriptMode.
const (
	ScriptsModeNonconcurrent = "nonconcurrent"
	ScriptsModeConcurrent    = "concurrent"
)

// ScriptsDialog is a modal editor for a workspace's user-entered setup/run/
// archive/on-done script commands plus its run-mode. Rows are fixed (unlike
// the env dialog's dynamic key/value list): Up/Down moves a field cursor,
// printable runes edit the focused command, Backspace deletes the last rune,
// and the mode row toggles with left/right/space. Esc always cancels.
//
// The dialog renders the trust caveat inline: these commands run WITHOUT a
// trust prompt (unlike repo-provided .amux/workspaces.json scripts), so the
// field is where a user types their own commands -- never somewhere they
// paste a repo-supplied one.
type ScriptsDialog struct {
	visible bool
	width   int

	setup   string
	run     string
	archive string
	onDone  string
	mode    string
	cursor  int // 0=setup 1=run 2=archive 3=on-done 4=mode
}

// NewScriptsDialog seeds the dialog from the workspace's current values.
func NewScriptsDialog(setup, run, archive, onDone, mode string) *ScriptsDialog {
	if mode != ScriptsModeConcurrent {
		mode = ScriptsModeNonconcurrent
	}
	return &ScriptsDialog{setup: setup, run: run, archive: archive, onDone: onDone, mode: mode}
}

func (d *ScriptsDialog) Show()            { d.visible = true }
func (d *ScriptsDialog) Hide()            { d.visible = false }
func (d *ScriptsDialog) Visible() bool    { return d.visible }
func (d *ScriptsDialog) SetSize(w, _ int) { d.width = w }
func (d *ScriptsDialog) Cursor() *tea.Cursor {
	return nil
}

// Values returns the edited fields for read-back on close.
func (d *ScriptsDialog) Values() (setup, run, archive, onDone, mode string) {
	return d.setup, d.run, d.archive, d.onDone, d.mode
}

func (d *ScriptsDialog) Update(msg tea.Msg) (*ScriptsDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}

	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		d.visible = false
		return d, func() tea.Msg { return ScriptsDialogResult{Canceled: true} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		d.visible = false
		return d, func() tea.Msg { return ScriptsDialogResult{} }

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("down"))):
		d.cursor = (d.cursor + 1) % 5
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("up"))):
		d.cursor = (d.cursor + 4) % 5
		return d, nil

	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("backspace"))):
		d.deleteFocusedRune()
		return d, nil
	}

	// The mode row is a toggle, not a text field: space/left/right flip it
	// and swallow the keystroke so " " can't land in a command.
	if d.cursor == 4 {
		if keyMsg.Text == " " || keyMsg.Code == tea.KeyLeft || keyMsg.Code == tea.KeyRight {
			d.toggleMode()
		}
		return d, nil
	}
	if keyMsg.Text != "" {
		d.appendFocusedText(keyMsg.Text)
	}
	return d, nil
}

func (d *ScriptsDialog) focusedValue() *string {
	switch d.cursor {
	case 0:
		return &d.setup
	case 1:
		return &d.run
	case 2:
		return &d.archive
	case 3:
		return &d.onDone
	}
	return nil
}

func (d *ScriptsDialog) appendFocusedText(txt string) {
	if v := d.focusedValue(); v != nil {
		*v += keepRunes(txt, isPrintableFieldRune)
	}
}

func (d *ScriptsDialog) deleteFocusedRune() {
	if v := d.focusedValue(); v != nil {
		*v = trimLastRune(*v)
	}
}

func (d *ScriptsDialog) toggleMode() {
	if d.mode == ScriptsModeConcurrent {
		d.mode = ScriptsModeNonconcurrent
	} else {
		d.mode = ScriptsModeConcurrent
	}
}

func (d *ScriptsDialog) View() string {
	if !d.visible {
		return ""
	}
	w := 40
	if d.width > 0 {
		w = min(60, max(40, d.width-20))
	}
	return dialogBorderStyle(w).Render(strings.Join(d.renderLines(), "\n"))
}

func (d *ScriptsDialog) renderLines() []string {
	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary())
	muted := lipgloss.NewStyle().Foreground(ColorMuted())
	warn := lipgloss.NewStyle().Foreground(ColorWarning())

	lines := []string{
		title.Render("Workspace Scripts"),
		warn.Render("These commands run without a trust prompt."),
		muted.Render("Enter only your own commands."),
		"",
	}

	rows := []struct {
		label string
		value string
	}{
		{"setup", d.setup},
		{"run", d.run},
		{"archive", d.archive},
		{"on-done", d.onDone},
		{"mode", d.mode},
	}
	for i, r := range rows {
		style, prefix := lipgloss.NewStyle(), "  "
		if i == d.cursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		value := r.value
		if i == 4 {
			value = "< " + r.value + " >"
		}
		lines = append(lines, prefix+style.Render(r.label+": ")+style.Render(value))
	}

	lines = append(lines, "", muted.Render("up/down move  type to edit  space toggles mode  enter save  esc cancel"))
	return lines
}
