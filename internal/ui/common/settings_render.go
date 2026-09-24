package common

import (
	"charm.land/lipgloss/v2"
)

func (s *SettingsDialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	return dialogFrameOffsets(s.dialogStyle())
}

func (s *SettingsDialog) dialogBounds(contentHeight int) (x, y, w, h int) {
	frameX, frameY, _, _ := s.dialogFrame()
	return centerDialogBounds(s.width, s.height, s.dialogContentWidth(), frameX, frameY, contentHeight)
}

func (s *SettingsDialog) addHit(item settingsItem, index, y int) {
	s.hitRegions = append(s.hitRegions, settingsHitRegion{
		item: item, index: index,
		region: HitRegion{X: 0, Y: y, Width: s.dialogContentWidth(), Height: 1},
	})
}

func (s *SettingsDialog) renderLines() []string {
	s.hitRegions = s.hitRegions[:0]
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary())
	label := lipgloss.NewStyle().Foreground(ColorMuted())
	muted := lipgloss.NewStyle().Foreground(ColorMuted())

	lines = append(lines, title.Render("Settings"), "")

	lines = append(lines, label.Render("Theme"))
	for i, t := range s.themes {
		style, prefix := muted, "  "
		if i == s.themeCursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		y := len(lines)
		lines = append(lines, prefix+style.Render(t.Name))
		s.addHit(settingsItemTheme, i, y)
	}
	lines = append(lines, "")

	// Tmux section. These persist to config and are read at launch, so the
	// header carries a restart hint. Each field is an editable, focusable row.
	lines = append(lines, label.Render("Tmux ")+muted.Render("(restart to apply)"))
	tmuxFields := []struct {
		item  settingsItem
		name  string
		value string
	}{
		{settingsItemTmuxServer, "Server", s.tmuxServer},
		{settingsItemTmuxConfig, "Config", s.tmuxConfigPath},
		{settingsItemTmuxSync, "Sync interval", s.tmuxSyncInterval},
	}
	for _, f := range tmuxFields {
		style, prefix := muted, "  "
		if s.focusedItem == f.item {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		y := len(lines)
		lines = append(lines, prefix+style.Render(f.name+": "+f.value))
		s.addHit(f.item, -1, y)
	}
	lines = append(lines, "")

	lines = s.renderAssistantLines(lines, label, muted)

	lines = append(lines, label.Render("Version"))
	if s.currentVersion == "" || s.currentVersion == "dev" {
		lines = append(lines, muted.Render("  Development build"))
	} else {
		lines = append(lines, muted.Render("  "+s.currentVersion))
	}
	if s.updateHint != "" {
		lines = append(lines, muted.Render("  "+s.updateHint))
	}

	if s.updateAvailable {
		style := lipgloss.NewStyle().Foreground(ColorSuccess())
		if s.focusedItem == settingsItemUpdate {
			style = style.Bold(true)
		}
		y := len(lines)
		lines = append(lines, style.Render("  [Update to "+s.latestVersion+"]"))
		s.addHit(settingsItemUpdate, -1, y)
	}
	lines = append(lines, "")

	style := muted
	if s.focusedItem == settingsItemClose {
		style = lipgloss.NewStyle().Foreground(ColorPrimary())
	}
	y := len(lines)
	lines = append(lines, style.Render("[Close]"))
	s.addHit(settingsItemClose, -1, y)

	return lines
}

// renderAssistantLines appends the Assistants section: one row per roster
// entry (name + editable command). Rendered whenever a roster was set via
// SetAssistants (production always sets one; dialogs built directly in tests
// without it show no section, matching how an empty theme/update list would
// render). An empty roster still shows the header plus a hint so the ctrl+a
// first-add affordance is discoverable. The add input and post-add notice
// render inline.
func (s *SettingsDialog) renderAssistantLines(lines []string, label, muted lipgloss.Style) []string {
	if !s.assistantsSet && !s.assistantAdding {
		return lines
	}
	lines = append(lines, label.Render("Assistants"))
	if len(s.assistantNames) == 0 && !s.assistantAdding {
		style, prefix := muted, "  "
		if s.focusedItem == settingsItemAssistants {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		lines = append(lines, prefix+style.Render("(none — ctrl+a to add)"))
	}
	for i, name := range s.assistantNames {
		style, prefix := muted, "  "
		if s.focusedItem == settingsItemAssistants && i == s.assistantCursor && !s.assistantAdding {
			style = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			prefix = Icons.Cursor + " "
		}
		y := len(lines)
		lines = append(lines, prefix+style.Render(name+": "+s.assistantCommands[name]))
		s.addHit(settingsItemAssistants, i, y)
	}
	if s.assistantAdding {
		lines = s.renderAssistantAddLines(lines, muted)
	} else if s.assistantNotice != "" {
		lines = append(lines, muted.Render("  "+s.assistantNotice))
	}
	return append(lines, "")
}

// renderAssistantAddLines renders the two-field add input under the roster
// rows, highlighting whichever field is active while the section is focused.
func (s *SettingsDialog) renderAssistantAddLines(lines []string, muted lipgloss.Style) []string {
	nameStyle, cmdStyle := muted, muted
	namePrefix, cmdPrefix := "  ", "  "
	if s.focusedItem == settingsItemAssistants {
		if s.assistantAddField == 0 {
			nameStyle = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			namePrefix = Icons.Cursor + " "
		} else {
			cmdStyle = lipgloss.NewStyle().Foreground(ColorPrimary()).Bold(true)
			cmdPrefix = Icons.Cursor + " "
		}
	}
	lines = append(lines,
		muted.Render("  New assistant"),
		namePrefix+nameStyle.Render("name:    "+s.assistantAddName),
		cmdPrefix+cmdStyle.Render("command: "+s.assistantAddCmd))
	if s.assistantAddError != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorError()).Render("  "+s.assistantAddError))
	}
	return lines
}
