package common

import (
	"charm.land/lipgloss/v2"
)

// RunSessionPickerDialogID is the dialog ID assigned to the run-session
// picker produced by NewRunSessionPicker. The app layer matches on it to
// route confirm results, and dialog rendering branches on it for the
// picker's vertical layout (the shared select render is horizontal).
const RunSessionPickerDialogID = "run-session-picker"

// SessionPickerRow is one pickable session: Label is the rendered text
// (status included), Live marks a still-running session so the row can be
// styled differently from exited ones.
type SessionPickerRow struct {
	Label string
	Live  bool
}

// NewRunSessionPicker builds a vertical select dialog over the workspace's
// run sessions. The caller keeps the parallel session list and maps the
// result's Index back to a session name.
func NewRunSessionPicker(title, message string, rows []SessionPickerRow) *Dialog {
	options := make([]string, len(rows))
	live := make([]bool, len(rows))
	for i, r := range rows {
		options[i] = r.Label
		live[i] = r.Live
	}
	return &Dialog{
		id:             RunSessionPickerDialogID,
		dtype:          DialogSelect,
		title:          title,
		message:        message,
		options:        options,
		sessionRowLive: live,
	}
}

func (d *Dialog) renderSessionPickerOptions(baseLine int) []string {
	lines := []string{}
	for i, opt := range d.options {
		cursor := Icons.CursorEmpty + " "
		nameStyle := lipgloss.NewStyle().Foreground(ColorForeground())
		if i == d.cursor {
			cursor = Icons.Cursor + " "
			nameStyle = nameStyle.Bold(true)
		}
		indicator := lipgloss.NewStyle().Foreground(ColorMuted()).Render(Icons.Idle)
		if i < len(d.sessionRowLive) && d.sessionRowLive[i] {
			indicator = lipgloss.NewStyle().Foreground(ColorSuccess()).Render(Icons.Running)
		}
		line := cursor + indicator + " " + nameStyle.Render(SanitizeDisplayText(opt, dialogMaxLineRunes))
		d.addOptionHit(i, i, baseLine, 0, d.dialogContentWidth())
		lines = append(lines, line)
		baseLine++
	}
	return lines
}
