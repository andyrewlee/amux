package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SettingsResult is sent when the settings dialog is closed.
type SettingsResult struct {
	Confirmed          bool
	Theme              ThemeID
	ShowKeymapHints    bool
	HideSidebar        bool
	HideTerminal       bool
	AutoStartAgent     bool
	SyncProfilePlugins bool
	GlobalPermissions  bool
	NotificationSound  string
	TmuxPersistence    bool
}

// ShowPermissionsEditor is sent when the user clicks "Edit Global Allow/Deny List".
type ShowPermissionsEditor struct{}

// ShowSandboxRulesEditor is sent when the user clicks "Edit Sandbox Path Rules".
type ShowSandboxRulesEditor struct{}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme ThemeID
}

type settingsItem int

const (
	settingsItemKeymap settingsItem = iota
	settingsItemHideSidebar
	settingsItemHideTerminal
	settingsItemSyncPlugins         // Shared Config section
	settingsItemGlobalPerms
	settingsItemEditPermissions
	settingsItemNotificationSound   // Agents section
	settingsItemEditSandboxRules
	settingsItemAutoStart           // Tmux section
	settingsItemTmuxPersistence
	settingsItemManageProfiles
	settingsItemEditTheme
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
	hideTerminal       bool
	autoStartAgent     bool
	syncProfilePlugins bool
	globalPerms        bool
	notificationSound  string
	tmuxPersistence    bool

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
func NewSettingsDialog(currentTheme ThemeID, showKeymapHints, hideSidebar, hideTerminal, autoStartAgent, syncProfilePlugins, globalPerms bool, notificationSound string, tmuxPersistence bool) *SettingsDialog {
	return &SettingsDialog{
		theme:              currentTheme,
		showKeymapHints:    showKeymapHints,
		hideSidebar:        hideSidebar,
		hideTerminal:       hideTerminal,
		autoStartAgent:     autoStartAgent,
		syncProfilePlugins: syncProfilePlugins,
		globalPerms:        globalPerms,
		notificationSound:  notificationSound,
		tmuxPersistence:    tmuxPersistence,
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
func (s *SettingsDialog) SetNotificationSound(sound string) { s.notificationSound = sound }

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

	case settingsItemHideTerminal:
		s.hideTerminal = !s.hideTerminal
		return s, nil

	case settingsItemNotificationSound:
		s.visible = false
		return s, func() tea.Msg { return ShowSoundPicker{} }

	case settingsItemAutoStart:
		s.autoStartAgent = !s.autoStartAgent
		return s, nil

	case settingsItemSyncPlugins:
		s.syncProfilePlugins = !s.syncProfilePlugins
		return s, nil

	case settingsItemManageProfiles:
		s.visible = false
		return s, func() tea.Msg { return ShowProfileManager{} }

	case settingsItemGlobalPerms:
		s.globalPerms = !s.globalPerms
		return s, nil

	case settingsItemEditPermissions:
		if s.globalPerms {
			s.visible = false
			return s, func() tea.Msg { return ShowPermissionsEditor{} }
		}
		return s, nil

	case settingsItemEditSandboxRules:
		s.visible = false
		return s, func() tea.Msg { return ShowSandboxRulesEditor{} }

	case settingsItemTmuxPersistence:
		s.tmuxPersistence = !s.tmuxPersistence
		return s, nil

	case settingsItemSave:
		s.visible = false
		return s, func() tea.Msg {
			return SettingsResult{
				Confirmed:          true,
				Theme:              s.theme,
				ShowKeymapHints:    s.showKeymapHints,
				HideSidebar:        s.hideSidebar,
				HideTerminal:       s.hideTerminal,
				AutoStartAgent:     s.autoStartAgent,
				SyncProfilePlugins: s.syncProfilePlugins,
				GlobalPermissions:  s.globalPerms,
				NotificationSound:  s.notificationSound,
				TmuxPersistence:    s.tmuxPersistence,
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
	// Skip edit permissions when global perms is off
	if !s.globalPerms && s.focusedItem == settingsItemEditPermissions {
		s.focusedItem = settingsItemNotificationSound
	}
}

func (s *SettingsDialog) skipDisabledBackward() {
	// Skip edit permissions when global perms is off
	if !s.globalPerms && s.focusedItem == settingsItemEditPermissions {
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
