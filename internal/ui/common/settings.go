package common

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// SettingsResult is sent when the settings dialog is closed. Canceled is true
// when the user dismissed via Esc (revert any live theme preview) rather than
// confirming via [Close].
type SettingsResult struct {
	Canceled bool
}

// ThemePreview is sent when user navigates through themes for live preview.
type ThemePreview struct {
	Theme   ThemeID
	Session int
}

type settingsItem int

const (
	settingsItemTheme settingsItem = iota
	settingsItemTmuxServer
	settingsItemTmuxConfig
	settingsItemTmuxSync
	settingsItemAssistants
	settingsItemUpdate // only shown when update available
	settingsItemClose
)

// isTmuxField reports whether item is one of the editable tmux text fields.
func isTmuxField(item settingsItem) bool {
	return item == settingsItemTmuxServer ||
		item == settingsItemTmuxConfig ||
		item == settingsItemTmuxSync
}

// SettingsDialog is a modal dialog for application settings.
type SettingsDialog struct {
	visible bool
	width   int
	height  int

	// Settings values
	theme ThemeID

	// tmux server / config / sync-interval values. These persist to
	// UISettings and are exported as AMUX_TMUX_* env vars at launch, so
	// edits here take effect on next start (surfaced by the section hint).
	tmuxServer       string
	tmuxConfigPath   string
	tmuxSyncInterval string

	// Assistant roster values. assistantNames is the fixed, ordered display
	// list for the dialog's lifetime (set via SetAssistants); assistantCommands
	// holds the (possibly edited) command string per assistant name, persisted
	// to the config's "assistants" map on close. Only an existing assistant's
	// command is editable in this first cut -- adding a brand-new assistant
	// name needs a different input model (a name field plus validation) and is
	// deferred; see plan 031.
	assistantNames    []string
	assistantCommands map[string]string
	assistantCursor   int

	// UI state
	focusedItem settingsItem
	themeCursor int
	themes      []Theme
	session     int

	// scrollOffset is the first visible row of the scrollable body (the
	// section between the fixed "Settings" header and the fixed "[Close]"
	// footer). It is recomputed on every render (see composeVisibleLines in
	// settings_scroll.go) so it always keeps the focused row in view,
	// without requiring every navigation handler to update it explicitly.
	scrollOffset int

	// For mouse hit detection
	hitRegions []settingsHitRegion

	// Update state
	currentVersion  string
	latestVersion   string
	updateAvailable bool
	updateHint      string
}

type settingsHitRegion struct {
	item   settingsItem
	index  int
	region HitRegion
}

// NewSettingsDialog creates a new settings dialog with current values.
func NewSettingsDialog(currentTheme ThemeID, tmuxServer, tmuxConfig, tmuxSync string) *SettingsDialog {
	themes := AvailableThemes()
	themeCursor := 0
	for i, t := range themes {
		if t.ID == currentTheme {
			themeCursor = i
			break
		}
	}

	return &SettingsDialog{
		theme:            currentTheme,
		tmuxServer:       tmuxServer,
		tmuxConfigPath:   tmuxConfig,
		tmuxSyncInterval: tmuxSync,
		themes:           themes,
		themeCursor:      themeCursor,
		focusedItem:      settingsItemTheme,
	}
}

func (s *SettingsDialog) Show()               { s.visible = true }
func (s *SettingsDialog) Hide()               { s.visible = false }
func (s *SettingsDialog) Visible() bool       { return s.visible }
func (s *SettingsDialog) SetSize(w, h int)    { s.width, s.height = w, h }
func (s *SettingsDialog) Cursor() *tea.Cursor { return nil }
func (s *SettingsDialog) SetSession(session int) {
	s.session = session
}

func (s *SettingsDialog) SelectedTheme() ThemeID {
	return s.theme
}

// TmuxServer, TmuxConfigPath, and TmuxSyncInterval return the current (possibly
// edited) tmux values so the app can persist them into UISettings on close.
func (s *SettingsDialog) TmuxServer() string       { return s.tmuxServer }
func (s *SettingsDialog) TmuxConfigPath() string   { return s.tmuxConfigPath }
func (s *SettingsDialog) TmuxSyncInterval() string { return s.tmuxSyncInterval }

func (s *SettingsDialog) SetSelectedTheme(theme ThemeID) {
	s.theme = theme
	for i, t := range s.themes {
		if t.ID == theme {
			s.themeCursor = i
			return
		}
	}
}

// SetUpdateInfo sets version information for the updates section.
func (s *SettingsDialog) SetUpdateInfo(current, latest string, available bool) {
	s.currentVersion = current
	s.latestVersion = latest
	s.updateAvailable = available
}

// SetUpdateHint sets a hint shown under the current version.
func (s *SettingsDialog) SetUpdateHint(hint string) {
	s.updateHint = strings.TrimSpace(hint)
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
		// Esc always cancels, whatever is focused.
		if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
			s.visible = false
			return s, func() tea.Msg { return SettingsResult{Canceled: true} }
		}

		// While a tmux text field is focused, printable keys (including j/k and
		// space) are text, so only structural keys navigate. Handle it before the
		// list-navigation switch so those characters are not swallowed as motions.
		if isTmuxField(s.focusedItem) {
			return s.handleTmuxFieldKey(msg)
		}
		if isAssistantsField(s.focusedItem) {
			return s.handleAssistantFieldKey(msg)
		}

		switch {
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

// handleTmuxFieldKey edits the focused tmux text field. Structural keys move
// between fields/sections; backspace deletes; any other printable text is
// appended (filtered to valid characters for the field).
func (s *SettingsDialog) handleTmuxFieldKey(msg tea.KeyPressMsg) (*SettingsDialog, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "down", "enter"))):
		return s.handleNextSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab", "up"))):
		return s.handlePrevSection()

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		s.deleteFocusedTmuxRune()
		return s, nil
	}

	if msg.Text != "" {
		s.appendFocusedTmuxText(msg.Text)
	}
	return s, nil
}

// appendFocusedTmuxText appends filtered text to the focused tmux field. The
// sync-interval field only accepts characters that can appear in a Go duration
// so the UI cannot persist a value the consumer would reject and silently
// replace with its default.
func (s *SettingsDialog) appendFocusedTmuxText(txt string) {
	switch s.focusedItem {
	case settingsItemTmuxServer:
		s.tmuxServer += keepRunes(txt, isPrintableFieldRune)
	case settingsItemTmuxConfig:
		s.tmuxConfigPath += keepRunes(txt, isPrintableFieldRune)
	case settingsItemTmuxSync:
		s.tmuxSyncInterval += keepRunes(txt, isDurationRune)
	}
}

// deleteFocusedTmuxRune removes the last rune from the focused tmux field.
func (s *SettingsDialog) deleteFocusedTmuxRune() {
	switch s.focusedItem {
	case settingsItemTmuxServer:
		s.tmuxServer = trimLastRune(s.tmuxServer)
	case settingsItemTmuxConfig:
		s.tmuxConfigPath = trimLastRune(s.tmuxConfigPath)
	case settingsItemTmuxSync:
		s.tmuxSyncInterval = trimLastRune(s.tmuxSyncInterval)
	}
}

func keepRunes(s string, keep func(rune) bool) string {
	var b strings.Builder
	for _, r := range s {
		if keep(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}

// isPrintableFieldRune accepts any printable rune (spaces included, so paths and
// server names with spaces work) while rejecting control characters.
func isPrintableFieldRune(r rune) bool {
	return unicode.IsGraphic(r)
}

// isDurationRune accepts only characters that can appear in a Go duration string
// (time.ParseDuration): digits, a decimal point, and lowercase unit letters
// covering ns, us/µs, ms, s, m, and h.
func isDurationRune(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune(".nsuµmh", r)
}

func (s *SettingsDialog) handleSelect() (*SettingsDialog, tea.Cmd) {
	switch s.focusedItem {
	case settingsItemTheme:
		if s.themeCursor >= 0 && s.themeCursor < len(s.themes) {
			s.theme = s.themes[s.themeCursor].ID
		}
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme, Session: s.session} }

	case settingsItemUpdate:
		if s.updateAvailable {
			s.visible = false
			return s, func() tea.Msg { return messages.TriggerUpgrade{} }
		}

	case settingsItemClose:
		s.visible = false
		return s, func() tea.Msg { return SettingsResult{} }
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
	// Skip update item if no update available, landing on the item before it.
	if s.focusedItem == settingsItemUpdate && !s.updateAvailable {
		s.focusedItem = settingsItemAssistants
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
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme, Session: s.session} }
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
		return s, func() tea.Msg { return ThemePreview{Theme: s.theme, Session: s.session} }
	}
	return s.handlePrevSection()
}

func (s *SettingsDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	// composeVisibleLines is what View() actually renders (a height-clamped,
	// scroll-offset window of the body plus the always-visible footer), and
	// it remaps s.hitRegions to match those on-screen coordinates. Using the
	// raw, unclamped renderLines() here would let clicks resolve against
	// rows that are currently scrolled out of view.
	lines := s.composeVisibleLines()
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
			if hit.item == settingsItemAssistants && hit.index >= 0 {
				s.assistantCursor = hit.index
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
	return s.dialogStyle().Render(strings.Join(s.composeVisibleLines(), "\n"))
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
		BorderForeground(ColorPrimary()).
		Padding(1, 2).
		Width(s.dialogContentWidth())
}
