package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
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

type settingsItem int

const (
	settingsItemTheme settingsItem = iota
	settingsItemKeymap
	settingsItemUpdate // only shown when update available
	settingsItemClose
)

// SettingsDialog is a modal dialog for application settings.
type SettingsDialog struct {
	visible bool
	width   int
	height  int

	// Settings values
	theme           ThemeID
	showKeymapHints bool
	originalTheme   ThemeID

	// UI state
	focusedItem settingsItem
	themeCursor int
	themes      []Theme

	// For mouse hit detection
	hitRegions        []settingsHitRegion
	showKeymapHintsUI bool

	// Update state
	currentVersion  string
	latestVersion   string
	updateAvailable bool
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

func (s *SettingsDialog) Show()                        { s.visible = true; s.originalTheme = s.theme }
func (s *SettingsDialog) Hide()                        { s.visible = false }
func (s *SettingsDialog) Visible() bool                { return s.visible }
func (s *SettingsDialog) SetSize(w, h int)             { s.width, s.height = w, h }
func (s *SettingsDialog) SetShowKeymapHints(show bool) { s.showKeymapHintsUI = show }
func (s *SettingsDialog) Cursor() *tea.Cursor          { return nil }

// SetUpdateInfo sets version information for the updates section.
func (s *SettingsDialog) SetUpdateInfo(current, latest string, available bool) {
	s.currentVersion = current
	s.latestVersion = latest
	s.updateAvailable = available
}

// Update handles input.
func (s *SettingsDialog) Update(msg tea.Msg) (*SettingsDialog, tea.Cmd) {
	if !s.visible {
		return s, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return s, s.handleClick(msg)
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			s.theme = s.originalTheme
			s.visible = false
			return s, func() tea.Msg {
				return SafeBatch(
					func() tea.Msg { return ThemePreview{Theme: s.originalTheme} },
					func() tea.Msg { return SettingsResult{Confirmed: false} },
				)()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			return s.handleSelect()

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			return s.handleNextSection()

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
			return s.handlePrevSection()

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			return s.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			return s.handlePrev()
		}
	}

	return s, nil
}

func (s *SettingsDialog) handleSelect() (*SettingsDialog, tea.Cmd) {
	switch s.focusedItem {
	case settingsItemTheme:
		if s.themeCursor >= 0 && s.themeCursor < len(s.themes) {
			s.theme = s.themes[s.themeCursor].ID
		}
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{Confirmed: true, Theme: s.theme, ShowKeymapHints: s.showKeymapHints}
		}

	case settingsItemKeymap:
		s.showKeymapHints = !s.showKeymapHints
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{Confirmed: true, Theme: s.theme, ShowKeymapHints: s.showKeymapHints}
		}

	case settingsItemUpdate:
		if s.updateAvailable {
			s.visible = false
			return s, func() tea.Msg { return messages.TriggerUpgrade{} }
		}

	case settingsItemClose:
		s.visible = false
		return s, func() tea.Msg { return SettingsResult{Confirmed: false} }
	}
	return s, nil
}

// handleNextSection moves focus to the next section (Tab key).
func (s *SettingsDialog) handleNextSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem++
	// Skip update item if no update available
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemClose
	}
	if s.focusedItem > settingsItemClose {
		s.focusedItem = settingsItemTheme
	}
	return s, nil
}

// handlePrevSection moves focus to the previous section (Shift+Tab key).
func (s *SettingsDialog) handlePrevSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem--
	// Skip update item if no update available
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemKeymap
	}
	if s.focusedItem < 0 {
		s.focusedItem = settingsItemClose
	}
	return s, nil
}

// handleNext cycles within the current section (down/j keys).
// For theme section, cycles through themes. For others, moves to next section.
func (s *SettingsDialog) handleNext() (*SettingsDialog, tea.Cmd) {
	if s.focusedItem == settingsItemTheme {
		s.themeCursor = (s.themeCursor + 1) % len(s.themes)
		s.theme = s.themes[s.themeCursor].ID
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme} }
	}
	return s.handleNextSection()
}

// handlePrev cycles within the current section (up/k keys).
// For theme section, cycles through themes. For others, moves to previous section.
func (s *SettingsDialog) handlePrev() (*SettingsDialog, tea.Cmd) {
	if s.focusedItem == settingsItemTheme {
		s.themeCursor--
		if s.themeCursor < 0 {
			s.themeCursor = len(s.themes) - 1
		}
		s.theme = s.themes[s.themeCursor].ID
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme} }
	}
	return s.handlePrevSection()
}

func (s *SettingsDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	lines := s.renderLines()
	contentHeight := len(lines)
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
			s.focusedItem = hit.item
			if hit.item == settingsItemTheme && hit.index >= 0 {
				s.themeCursor = hit.index
			}
			_, cmd := s.handleSelect()
			return cmd
		}
	}
	return nil
}

func (s *SettingsDialog) View() string {
	if !s.visible {
		return ""
	}
	return s.dialogStyle().Render(strings.Join(s.renderLines(), "\n"))
}

func (s *SettingsDialog) dialogContentWidth() int {
	if s.width > 0 {
		return min(50, max(35, s.width-20))
	}
	return 40
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

	// Theme
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

	// Keymap
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

	// Version info
	lines = append(lines, label.Render("Version"))
	if s.currentVersion == "" || s.currentVersion == "dev" {
		lines = append(lines, muted.Render("  Development build"))
	} else {
		lines = append(lines, muted.Render("  "+s.currentVersion))
	}

	// Update button (only if available)
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

	// Close
	style = muted
	if s.focusedItem == settingsItemClose {
		style = lipgloss.NewStyle().Foreground(ColorPrimary)
	}
	y = len(lines)
	lines = append(lines, style.Render("[Esc] Close"))
	s.addHit(settingsItemClose, -1, y)

	if s.showKeymapHintsUI {
		lines = append(lines, "", muted.Render("↑/↓ navigate • Enter select"))
	}

	return lines
}
