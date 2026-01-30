package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// SettingsResult is sent when the settings dialog is closed.
type SettingsResult struct {
	Confirmed        bool
	Theme            ThemeID
	ShowKeymapHints  bool
	TmuxServer       string
	TmuxConfigPath   string
	TmuxSyncInterval string
}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme ThemeID
}

type settingsItem int

const (
	settingsItemTheme settingsItem = iota
	settingsItemKeymap
	settingsItemTmuxServer
	settingsItemTmuxConfig
	settingsItemTmuxSync
	settingsItemUpdate // only shown when update available
	settingsItemSave
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
	tmuxServer      textinput.Model
	tmuxConfig      textinput.Model
	tmuxSync        textinput.Model
	validationErr   string

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
func NewSettingsDialog(currentTheme ThemeID, showKeymapHints bool, tmuxServer, tmuxConfig, tmuxSync string) *SettingsDialog {
	themes := AvailableThemes()
	themeCursor := 0
	for i, t := range themes {
		if t.ID == currentTheme {
			themeCursor = i
			break
		}
	}

	serverInput := textinput.New()
	serverInput.Placeholder = "default"
	serverInput.SetWidth(24)
	serverInput.SetVirtualCursor(false)
	serverInput.SetValue(strings.TrimSpace(tmuxServer))

	configInput := textinput.New()
	configInput.Placeholder = "default"
	configInput.SetWidth(24)
	configInput.SetVirtualCursor(false)
	configInput.SetValue(strings.TrimSpace(tmuxConfig))

	syncInput := textinput.New()
	syncInput.Placeholder = "7s"
	syncInput.SetWidth(12)
	syncInput.SetVirtualCursor(false)
	syncInput.SetValue(strings.TrimSpace(tmuxSync))

	return &SettingsDialog{
		theme:           currentTheme,
		originalTheme:   currentTheme,
		showKeymapHints: showKeymapHints,
		tmuxServer:      serverInput,
		tmuxConfig:      configInput,
		tmuxSync:        syncInput,
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
	defer s.syncInputFocus()

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
		switch s.focusedItem {
		case settingsItemTmuxServer:
			var cmd tea.Cmd
			s.tmuxServer, cmd = s.tmuxServer.Update(msg)
			s.validationErr = ""
			return s, cmd
		case settingsItemTmuxConfig:
			var cmd tea.Cmd
			s.tmuxConfig, cmd = s.tmuxConfig.Update(msg)
			s.validationErr = ""
			return s, cmd
		case settingsItemTmuxSync:
			var cmd tea.Cmd
			s.tmuxSync, cmd = s.tmuxSync.Update(msg)
			s.validationErr = ""
			return s, cmd
		}
	case tea.PasteMsg:
		switch s.focusedItem {
		case settingsItemTmuxServer:
			var cmd tea.Cmd
			s.tmuxServer, cmd = s.tmuxServer.Update(msg)
			s.validationErr = ""
			return s, cmd
		case settingsItemTmuxConfig:
			var cmd tea.Cmd
			s.tmuxConfig, cmd = s.tmuxConfig.Update(msg)
			s.validationErr = ""
			return s, cmd
		case settingsItemTmuxSync:
			var cmd tea.Cmd
			s.tmuxSync, cmd = s.tmuxSync.Update(msg)
			s.validationErr = ""
			return s, cmd
		}
	}

	if s.validationErr != "" {
		s.validationErr = ""
	}

	return s, nil
}

func (s *SettingsDialog) handleSelect() (*SettingsDialog, tea.Cmd) {
	switch s.focusedItem {
	case settingsItemTheme:
		if s.themeCursor >= 0 && s.themeCursor < len(s.themes) {
			s.theme = s.themes[s.themeCursor].ID
		}
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme} }

	case settingsItemKeymap:
		s.showKeymapHints = !s.showKeymapHints
		return s, nil

	case settingsItemTmuxServer, settingsItemTmuxConfig, settingsItemTmuxSync:
		return s, nil

	case settingsItemUpdate:
		if s.updateAvailable {
			s.visible = false
			return s, func() tea.Msg { return messages.TriggerUpgrade{} }
		}

	case settingsItemSave:
		s.validationErr = s.validate()
		if s.validationErr != "" {
			return s, nil
		}
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{
				Confirmed:        true,
				Theme:            s.theme,
				ShowKeymapHints:  s.showKeymapHints,
				TmuxServer:       strings.TrimSpace(s.tmuxServer.Value()),
				TmuxConfigPath:   strings.TrimSpace(s.tmuxConfig.Value()),
				TmuxSyncInterval: strings.TrimSpace(s.tmuxSync.Value()),
			}
		}

	case settingsItemClose:
		s.theme = s.originalTheme
		s.visible = false
		return s, func() tea.Msg {
			return SafeBatch(
				func() tea.Msg { return ThemePreview{Theme: s.originalTheme} },
				func() tea.Msg { return SettingsResult{Confirmed: false} },
			)()
		}
	}
	return s, nil
}

// handleNextSection moves focus to the next section (Tab key).
func (s *SettingsDialog) handleNextSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem++
	// Skip update item if no update available
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemSave
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
		s.focusedItem = settingsItemTmuxSync
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

func (s *SettingsDialog) syncInputFocus() {
	if !s.visible {
		s.tmuxServer.Blur()
		s.tmuxConfig.Blur()
		s.tmuxSync.Blur()
		return
	}
	if s.focusedItem == settingsItemTmuxServer {
		s.tmuxServer.Focus()
	} else {
		s.tmuxServer.Blur()
	}
	if s.focusedItem == settingsItemTmuxConfig {
		s.tmuxConfig.Focus()
	} else {
		s.tmuxConfig.Blur()
	}
	if s.focusedItem == settingsItemTmuxSync {
		s.tmuxSync.Focus()
	} else {
		s.tmuxSync.Blur()
	}
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
