package common

import (
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
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
	return
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

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	label := lipgloss.NewStyle().Foreground(ColorMuted)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	lines = append(lines, title.Render("Settings"), "")

	contentWidth := s.dialogContentWidth()
	inputWidth := contentWidth - 14
	if inputWidth < 10 {
		inputWidth = 10
	}
	s.tmuxServer.SetWidth(inputWidth)
	s.tmuxConfig.SetWidth(inputWidth)
	s.tmuxSync.SetWidth(min(12, inputWidth))

	lines = append(lines, label.Render("Theme"))
	for i, t := range s.themes {
		style, prefix := muted, "  "
		if i == s.themeCursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
			prefix = Icons.Cursor + " "
		}
		y := len(lines)
		lines = append(lines, prefix+style.Render(t.Name))
		s.addHit(settingsItemTheme, i, y)
	}
	lines = append(lines, "")

	checkbox := "[ ]"
	if s.showKeymapHints {
		checkbox = "[" + Icons.Clean + "]"
	}
	style := lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemKeymap {
		style = style.Foreground(ColorPrimary)
	}
	y := len(lines)
	lines = append(lines, style.Render(checkbox+" Show keymap hints"))
	s.addHit(settingsItemKeymap, -1, y)
	lines = append(lines, "")

	lines = append(lines, label.Render("Tmux (Advanced)"))
	lines = append(lines, s.renderInputLine("Server", s.tmuxServer, s.focusedItem == settingsItemTmuxServer))
	s.addHit(settingsItemTmuxServer, -1, len(lines)-1)
	lines = append(lines, s.renderInputLine("Config", s.tmuxConfig, s.focusedItem == settingsItemTmuxConfig))
	s.addHit(settingsItemTmuxConfig, -1, len(lines)-1)
	lines = append(lines, s.renderInputLine("Sync interval", s.tmuxSync, s.focusedItem == settingsItemTmuxSync))
	s.addHit(settingsItemTmuxSync, -1, len(lines)-1)
	lines = append(lines, "")

	lines = append(lines, label.Render("Version"))
	if s.currentVersion == "" || s.currentVersion == "dev" {
		lines = append(lines, muted.Render("  Development build"))
	} else {
		lines = append(lines, muted.Render("  "+s.currentVersion))
	}

	if s.updateAvailable {
		style := lipgloss.NewStyle().Foreground(ColorSuccess)
		if s.focusedItem == settingsItemUpdate {
			style = style.Bold(true)
		}
		y = len(lines)
		lines = append(lines, style.Render("  [Update to "+s.latestVersion+"]"))
		s.addHit(settingsItemUpdate, -1, y)
	}
	lines = append(lines, "")

	style = muted
	if s.focusedItem == settingsItemSave {
		style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Save settings]"))
	s.addHit(settingsItemSave, -1, y)
	lines = append(lines, "")

	style = muted
	if s.focusedItem == settingsItemClose {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Esc] Close"))
	s.addHit(settingsItemClose, -1, y)

	if s.showKeymapHintsUI {
		if s.validationErr != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorError)
			lines = append(lines, "", errStyle.Render("! "+s.validationErr))
		} else {
			lines = append(lines, "", muted.Render("↑/↓ navigate • Enter select/save"))
		}
	}

	return lines
}

func (s *SettingsDialog) renderInputLine(labelText string, input textinput.Model, focused bool) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	if focused {
		labelStyle = labelStyle.Foreground(ColorPrimary)
	}
	return labelStyle.Render("  "+labelText+": ") + input.View()
}

func (s *SettingsDialog) validate() string {
	syncValue := strings.TrimSpace(s.tmuxSync.Value())
	if syncValue != "" {
		if d, err := time.ParseDuration(syncValue); err != nil || d <= 0 {
			return "tmux sync interval must be a valid duration (e.g. 7s)"
		}
	}
	configValue := strings.TrimSpace(s.tmuxConfig.Value())
	if configValue != "" {
		// Intentional: fail fast on missing config to avoid confusing tmux startup errors.
		if _, err := os.Stat(configValue); err != nil {
			return "tmux config path not found"
		}
	}
	return ""
}
