package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/messages"
)

// SettingsResult is sent when the settings dialog is closed.
type SettingsResult struct {
	Confirmed          bool
	Theme              ThemeID
	ShowKeymapHints    bool
	HideSidebar        bool
	AutoStartAgent     bool
	SyncProfilePlugins bool
	GlobalPermissions  bool
	AutoAddPermissions bool
	TmuxPersistence    bool
	TmuxServer         string
	TmuxConfigPath     string
	TmuxSyncInterval   string
}

// ShowPermissionsEditor is sent when the user clicks "Edit Global Allow/Deny List".
type ShowPermissionsEditor struct{}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme ThemeID
}

type settingsItem int

const (
	settingsItemKeymap settingsItem = iota
	settingsItemHideSidebar
	settingsItemSyncPlugins
	settingsItemGlobalPerms
	settingsItemEditPermissions
	settingsItemAutoAddPerms
	settingsItemAutoStart       // first item under Tmux (Advanced)
	settingsItemTmuxPersistence
	settingsItemTmuxServer
	settingsItemTmuxConfig
	settingsItemTmuxSync
	settingsItemUpdate    // only shown when update available
	settingsItemEditTheme // theme selection moved to bottom
	settingsItemSave
	settingsItemClose
)

// SettingsDialog is a modal dialog for application settings.
type SettingsDialog struct {
	visible bool
	width   int
	height  int

	// Settings values
	theme              ThemeID
	showKeymapHints    bool
	hideSidebar        bool
	autoStartAgent     bool
	syncProfilePlugins bool
	globalPerms        bool
	autoAddPerms       bool
	tmuxPersistence    bool
	tmuxServer         textinput.Model
	tmuxConfig         textinput.Model
	tmuxSync           textinput.Model
	validationErr      string

	// UI state
	focusedItem settingsItem

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
func NewSettingsDialog(currentTheme ThemeID, showKeymapHints, hideSidebar, autoStartAgent, syncProfilePlugins, globalPerms, autoAddPerms, tmuxPersistence bool, tmuxServer, tmuxConfig, tmuxSync string) *SettingsDialog {
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
		theme:              currentTheme,
		showKeymapHints:    showKeymapHints,
		hideSidebar:        hideSidebar,
		autoStartAgent:     autoStartAgent,
		syncProfilePlugins: syncProfilePlugins,
		globalPerms:        globalPerms,
		autoAddPerms:       autoAddPerms,
		tmuxPersistence:    tmuxPersistence,
		tmuxServer:         serverInput,
		tmuxConfig:         configInput,
		tmuxSync:           syncInput,
		focusedItem:        settingsItemKeymap,
	}
}

func (s *SettingsDialog) Show()                        { s.visible = true }
func (s *SettingsDialog) Hide()                        { s.visible = false }
func (s *SettingsDialog) Visible() bool                { return s.visible }
func (s *SettingsDialog) SetSize(w, h int)             { s.width, s.height = w, h }
func (s *SettingsDialog) SetShowKeymapHints(show bool) { s.showKeymapHintsUI = show }
func (s *SettingsDialog) Cursor() *tea.Cursor          { return nil }
func (s *SettingsDialog) SetTheme(theme ThemeID)       { s.theme = theme }

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
			s.visible = false
			return s, func() tea.Msg { return SettingsResult{Confirmed: false} }

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
	case settingsItemEditTheme:
		s.visible = false
		return s, func() tea.Msg { return ShowThemeEditor{} }

	case settingsItemKeymap:
		s.showKeymapHints = !s.showKeymapHints
		return s, nil

	case settingsItemHideSidebar:
		s.hideSidebar = !s.hideSidebar
		return s, nil

	case settingsItemAutoStart:
		s.autoStartAgent = !s.autoStartAgent
		return s, nil

	case settingsItemSyncPlugins:
		s.syncProfilePlugins = !s.syncProfilePlugins
		return s, nil

	case settingsItemGlobalPerms:
		s.globalPerms = !s.globalPerms
		return s, nil

	case settingsItemAutoAddPerms:
		if s.globalPerms {
			s.autoAddPerms = !s.autoAddPerms
		}
		return s, nil

	case settingsItemEditPermissions:
		if s.globalPerms {
			s.visible = false
			return s, func() tea.Msg { return ShowPermissionsEditor{} }
		}
		return s, nil

	case settingsItemTmuxPersistence:
		s.tmuxPersistence = !s.tmuxPersistence
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
				Confirmed:          true,
				Theme:              s.theme,
				ShowKeymapHints:    s.showKeymapHints,
				HideSidebar:        s.hideSidebar,
				AutoStartAgent:     s.autoStartAgent,
				SyncProfilePlugins: s.syncProfilePlugins,
				GlobalPermissions:  s.globalPerms,
				AutoAddPermissions: s.autoAddPerms,
				TmuxPersistence:    s.tmuxPersistence,
				TmuxServer:         strings.TrimSpace(s.tmuxServer.Value()),
				TmuxConfigPath:     strings.TrimSpace(s.tmuxConfig.Value()),
				TmuxSyncInterval:   strings.TrimSpace(s.tmuxSync.Value()),
			}
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
	s.skipDisabledForward()
	if s.focusedItem > settingsItemClose {
		s.focusedItem = settingsItemKeymap
	}
	return s, nil
}

// handlePrevSection moves focus to the previous section (Shift+Tab key).
func (s *SettingsDialog) handlePrevSection() (*SettingsDialog, tea.Cmd) {
	s.focusedItem--
	s.skipDisabledBackward()
	if s.focusedItem < 0 {
		s.focusedItem = settingsItemClose
	}
	return s, nil
}

func (s *SettingsDialog) skipDisabledForward() {
	// Skip edit permissions and auto-add when global perms is off
	if !s.globalPerms && (s.focusedItem == settingsItemEditPermissions || s.focusedItem == settingsItemAutoAddPerms) {
		s.focusedItem = settingsItemAutoStart
	}
	// Skip update item if no update available
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemEditTheme
	}
}

func (s *SettingsDialog) skipDisabledBackward() {
	// Skip update item if no update available
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemTmuxSync
	}
	// Skip auto-add and edit permissions when global perms is off
	if !s.globalPerms && (s.focusedItem == settingsItemAutoAddPerms || s.focusedItem == settingsItemEditPermissions) {
		s.focusedItem = settingsItemGlobalPerms
	}
	// Wrap around from before first item to last
	if s.focusedItem < 0 {
		s.focusedItem = settingsItemClose
	}
}

// handleNext moves to next item (down/j keys).
func (s *SettingsDialog) handleNext() (*SettingsDialog, tea.Cmd) {
	return s.handleNextSection()
}

// handlePrev moves to previous item (up/k keys).
func (s *SettingsDialog) handlePrev() (*SettingsDialog, tea.Cmd) {
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
