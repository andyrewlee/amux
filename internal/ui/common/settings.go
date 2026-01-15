package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SettingsResult is sent when the settings dialog is closed.
type SettingsResult struct {
	Confirmed       bool
	Theme           ThemeID
	ShowKeymapHints bool
}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme ThemeID
}

// settingsItem identifies a focusable item in the settings dialog.
type settingsItem int

const (
	settingsItemTheme settingsItem = iota
	settingsItemKeymap
	settingsItemClose
	settingsItemCount
)

// SettingsDialog is a modal dialog for application settings.
type SettingsDialog struct {
	visible bool
	width   int
	height  int

	// Settings values
	theme           ThemeID
	showKeymapHints bool
	originalTheme   ThemeID // for reverting on cancel

	// UI state
	focusedItem   settingsItem
	themeExpanded bool
	themeCursor   int
	themes        []Theme

	// For mouse hit detection
	hitRegions        []settingsHitRegion
	showKeymapHintsUI bool
}

type settingsHitRegion struct {
	item   settingsItem
	index  int
	region HitRegion
}

// NewSettingsDialog creates a new settings dialog with current values.
func NewSettingsDialog(currentTheme ThemeID, showKeymapHints bool) *SettingsDialog {
	themes := AvailableThemes()
	themeCursor := 0
	for i, t := range themes {
		if t.ID == currentTheme {
			themeCursor = i
			break
		}
	}

	return &SettingsDialog{
		theme:           currentTheme,
		originalTheme:   currentTheme,
		showKeymapHints: showKeymapHints,
		themes:          themes,
		themeCursor:     themeCursor,
		focusedItem:     settingsItemTheme,
	}
}

// Show makes the dialog visible.
func (s *SettingsDialog) Show() {
	s.visible = true
	s.focusedItem = settingsItemTheme
	s.themeExpanded = true // Start expanded so user can immediately browse
	s.originalTheme = s.theme
}

// Hide hides the dialog.
func (s *SettingsDialog) Hide() {
	s.visible = false
}

// Visible returns whether the dialog is visible.
func (s *SettingsDialog) Visible() bool {
	return s.visible
}

// SetSize sets the dialog size.
func (s *SettingsDialog) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// SetShowKeymapHints controls whether helper text is rendered.
func (s *SettingsDialog) SetShowKeymapHints(show bool) {
	s.showKeymapHintsUI = show
}

// Update handles messages.
func (s *SettingsDialog) Update(msg tea.Msg) (*SettingsDialog, tea.Cmd) {
	if !s.visible {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if cmd := s.handleClick(msg); cmd != nil {
				return s, cmd
			}
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			// Revert to original theme and close
			s.theme = s.originalTheme
			s.visible = false
			return s, func() tea.Msg {
				return tea.Batch(
					func() tea.Msg { return ThemePreview{Theme: s.originalTheme} },
					func() tea.Msg { return SettingsResult{Confirmed: false} },
				)()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return s.handleEnter()

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down", "j"))):
			return s.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up", "k"))):
			return s.handlePrev()

		case key.Matches(msg, key.NewBinding(key.WithKeys(" "))):
			if s.focusedItem == settingsItemKeymap {
				s.showKeymapHints = !s.showKeymapHints
				// Auto-save keymap change
				s.visible = false
				return s, func() tea.Msg {
					return SettingsResult{
						Confirmed:       true,
						Theme:           s.theme,
						ShowKeymapHints: s.showKeymapHints,
					}
				}
			} else if s.focusedItem == settingsItemTheme {
				s.themeExpanded = !s.themeExpanded
			}
		}
	}

	return s, nil
}

func (s *SettingsDialog) handleEnter() (*SettingsDialog, tea.Cmd) {
	switch s.focusedItem {
	case settingsItemTheme:
		if s.themeExpanded {
			// Select theme and auto-save
			if s.themeCursor >= 0 && s.themeCursor < len(s.themes) {
				s.theme = s.themes[s.themeCursor].ID
			}
			s.visible = false
			return s, func() tea.Msg {
				return SettingsResult{
					Confirmed:       true,
					Theme:           s.theme,
					ShowKeymapHints: s.showKeymapHints,
				}
			}
		} else {
			s.themeExpanded = true
		}
	case settingsItemKeymap:
		s.showKeymapHints = !s.showKeymapHints
		// Auto-save
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{
				Confirmed:       true,
				Theme:           s.theme,
				ShowKeymapHints: s.showKeymapHints,
			}
		}
	case settingsItemClose:
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{Confirmed: false}
		}
	}
	return s, nil
}

func (s *SettingsDialog) handleNext() (*SettingsDialog, tea.Cmd) {
	if s.themeExpanded && s.focusedItem == settingsItemTheme {
		s.themeCursor = (s.themeCursor + 1) % len(s.themes)
		// Live preview
		s.theme = s.themes[s.themeCursor].ID
		return s, func() tea.Msg {
			return ThemePreview{Theme: s.theme}
		}
	} else {
		s.focusedItem = (s.focusedItem + 1) % settingsItemCount
		if s.focusedItem == settingsItemTheme {
			s.themeExpanded = true
		} else {
			s.themeExpanded = false
		}
	}
	return s, nil
}

func (s *SettingsDialog) handlePrev() (*SettingsDialog, tea.Cmd) {
	if s.themeExpanded && s.focusedItem == settingsItemTheme {
		s.themeCursor--
		if s.themeCursor < 0 {
			s.themeCursor = len(s.themes) - 1
		}
		// Live preview
		s.theme = s.themes[s.themeCursor].ID
		return s, func() tea.Msg {
			return ThemePreview{Theme: s.theme}
		}
	} else {
		s.focusedItem--
		if s.focusedItem < 0 {
			s.focusedItem = settingsItemCount - 1
		}
		if s.focusedItem == settingsItemTheme {
			s.themeExpanded = true
		} else {
			s.themeExpanded = false
		}
	}
	return s, nil
}

func (s *SettingsDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if !s.visible {
		return nil
	}

	contentHeight := len(s.renderLines())
	if contentHeight == 0 {
		return nil
	}

	dialogX, dialogY, dialogW, dialogH := s.dialogBounds(contentHeight)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	_, _, contentOffsetX, contentOffsetY := s.dialogFrame()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range s.hitRegions {
		if hit.region.Contains(localX, localY) {
			switch hit.item {
			case settingsItemTheme:
				if hit.index >= 0 && hit.index < len(s.themes) {
					// Click on theme option - select and save
					s.theme = s.themes[hit.index].ID
					s.themeCursor = hit.index
					s.visible = false
					return func() tea.Msg {
						return SettingsResult{
							Confirmed:       true,
							Theme:           s.theme,
							ShowKeymapHints: s.showKeymapHints,
						}
					}
				}
			case settingsItemKeymap:
				s.showKeymapHints = !s.showKeymapHints
				s.visible = false
				return func() tea.Msg {
					return SettingsResult{
						Confirmed:       true,
						Theme:           s.theme,
						ShowKeymapHints: s.showKeymapHints,
					}
				}
			case settingsItemClose:
				s.visible = false
				return func() tea.Msg {
					return SettingsResult{Confirmed: false}
				}
			}
		}
	}

	return nil
}

// View renders the dialog.
func (s *SettingsDialog) View() string {
	if !s.visible {
		return ""
	}

	lines := s.renderLines()
	content := strings.Join(lines, "\n")
	return s.dialogStyle().Render(content)
}

// Cursor returns nil as settings dialog has no text input cursor.
func (s *SettingsDialog) Cursor() *tea.Cursor {
	return nil
}

func (s *SettingsDialog) dialogContentWidth() int {
	width := 40
	if s.width > 0 {
		width = min(50, max(35, s.width-20))
	}
	return width
}

func (s *SettingsDialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(s.dialogContentWidth())
}

func (s *SettingsDialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = s.dialogStyle().GetFrameSize()
	offsetX = frameX / 2
	offsetY = frameY / 2
	return frameX, frameY, offsetX, offsetY
}

func (s *SettingsDialog) dialogBounds(contentHeight int) (x, y, w, h int) {
	contentWidth := s.dialogContentWidth()
	frameX, frameY, _, _ := s.dialogFrame()
	w = contentWidth + frameX
	h = contentHeight + frameY
	x = (s.width - w) / 2
	y = (s.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

func (s *SettingsDialog) renderLines() []string {
	s.hitRegions = s.hitRegions[:0]
	lines := []string{}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary)
	lines = append(lines, titleStyle.Render("Settings"))
	lines = append(lines, "")

	// Theme section
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	lines = append(lines, labelStyle.Render("Theme"))

	// Theme options (always shown)
	for i, t := range s.themes {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(ColorMuted)
		if i == s.themeCursor {
			prefix = Icons.Cursor + " "
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
		}
		optionLine := prefix + style.Render(t.Name)
		optionY := len(lines)
		lines = append(lines, optionLine)

		s.hitRegions = append(s.hitRegions, settingsHitRegion{
			item:  settingsItemTheme,
			index: i,
			region: HitRegion{
				X:      0,
				Y:      optionY,
				Width:  s.dialogContentWidth(),
				Height: 1,
			},
		})
	}

	lines = append(lines, "")

	// Keymap toggle
	keymapY := len(lines)
	checkbox := "[ ]"
	if s.showKeymapHints {
		checkbox = "[" + Icons.Clean + "]"
	}
	keymapStyle := lipgloss.NewStyle().Foreground(ColorForeground)
	if s.focusedItem == settingsItemKeymap {
		keymapStyle = keymapStyle.Foreground(ColorPrimary)
	}
	lines = append(lines, keymapStyle.Render(checkbox+" Show keymap hints"))

	s.hitRegions = append(s.hitRegions, settingsHitRegion{
		item: settingsItemKeymap,
		region: HitRegion{
			X:      0,
			Y:      keymapY,
			Width:  s.dialogContentWidth(),
			Height: 1,
		},
	})

	lines = append(lines, "")

	// Close button
	closeY := len(lines)
	closeStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	if s.focusedItem == settingsItemClose {
		closeStyle = closeStyle.Foreground(ColorPrimary)
	}
	lines = append(lines, closeStyle.Render("[Esc] Close"))

	s.hitRegions = append(s.hitRegions, settingsHitRegion{
		item: settingsItemClose,
		region: HitRegion{
			X:      0,
			Y:      closeY,
			Width:  s.dialogContentWidth(),
			Height: 1,
		},
	})

	// Help text
	if s.showKeymapHintsUI {
		lines = append(lines, "")
		helpStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		lines = append(lines, helpStyle.Render("↑/↓: browse • Enter: select"))
	}

	return lines
}
