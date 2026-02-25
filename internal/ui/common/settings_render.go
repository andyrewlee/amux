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

	// ── General ──────────────────────────────────────────────
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

	checkbox = "[ ]"
	if s.hideSidebar {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemHideSidebar {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Hide sidebar"))
	s.addHit(settingsItemHideSidebar, -1, y)

	checkbox = "[ ]"
	if s.hideTerminal {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemHideTerminal {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Hide terminal"))
	s.addHit(settingsItemHideTerminal, -1, y)
	lines = append(lines, "")

	// ── Shared Config ────────────────────────────────────────
	lines = append(lines, label.Render("Shared Config"))

	checkbox = "[ ]"
	if s.syncProfilePlugins {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemSyncPlugins {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Sync plugins & skills across profiles"))
	s.addHit(settingsItemSyncPlugins, -1, y)

	checkbox = "[ ]"
	if s.globalPerms {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemGlobalPerms {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Global allow/deny list"))
	s.addHit(settingsItemGlobalPerms, -1, y)
	lines = append(lines, muted.Render("  Share permissions across sessions"))

	if s.globalPerms {
		style = muted
		if s.focusedItem == settingsItemEditPermissions {
			style = lipgloss.NewStyle().Foreground(ColorPrimary)
		}
		y = len(lines)
		lines = append(lines, style.Render("  [Edit Global Allow/Deny List]"))
		s.addHit(settingsItemEditPermissions, -1, y)
	} else {
		disabledStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		lines = append(lines, disabledStyle.Render("  [Edit Global Allow/Deny List]"))
	}
	lines = append(lines, "")

	// ── Agents ───────────────────────────────────────────────
	lines = append(lines, label.Render("Agents"))

	// Notification sound link
	soundLabel := "None"
	if s.notificationSound != "" {
		soundLabel = s.notificationSound
	}
	style = muted
	if s.focusedItem == settingsItemNotificationSound {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Notification sound: "+soundLabel+"]"))
	s.addHit(settingsItemNotificationSound, -1, y)

	// Sandbox rules link
	style = muted
	if s.focusedItem == settingsItemEditSandboxRules {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Edit Sandbox Path Rules]"))
	s.addHit(settingsItemEditSandboxRules, -1, y)
	lines = append(lines, "")

	// ── Tmux ─────────────────────────────────────────────────
	lines = append(lines, label.Render("Tmux"))

	checkbox = "[ ]"
	if s.autoStartAgent {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemAutoStart {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Auto start agent in new worktrees"))
	s.addHit(settingsItemAutoStart, -1, y)

	checkbox = "[ ]"
	if s.tmuxPersistence {
		checkbox = "[" + Icons.Clean + "]"
	}
	style = lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemTmuxPersistence {
		style = style.Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render(checkbox+" Keep sessions alive across restarts"))
	s.addHit(settingsItemTmuxPersistence, -1, y)
	lines = append(lines, "")

	// ── Other ────────────────────────────────────────────────

	// Manage Profiles link
	style = muted
	if s.focusedItem == settingsItemManageProfiles {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Manage Profiles]"))
	s.addHit(settingsItemManageProfiles, -1, y)

	// Theme link - shows current theme name
	currentTheme := GetTheme(s.theme)
	themeStyle := muted
	if s.focusedItem == settingsItemEditTheme {
		themeStyle = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, themeStyle.Render("[Change Theme: "+currentTheme.Name+"]"))
	s.addHit(settingsItemEditTheme, -1, y)
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
		lines = append(lines, "", muted.Render("↑/↓ navigate • Enter select/save"))
	}

	return lines
}
