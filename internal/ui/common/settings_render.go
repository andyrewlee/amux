package common

import (
	"charm.land/lipgloss/v2"
)

func (s *SettingsDialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = s.dialogStyle().GetFrameSize()
	return frameX, frameY, frameX / 2, frameY / 2
}

func (s *SettingsDialog) dialogBounds(contentHeight int) (x, y, w, h int) {
	contentWidth := s.dialogContentWidth()
	frameX, frameY, _, _ := s.dialogFrame()
	w, h = contentWidth+frameX, contentHeight+frameY
	x, y = (s.width-w)/2, (s.height-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
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
