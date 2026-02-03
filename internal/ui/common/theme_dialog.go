package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ShowThemeEditor is sent when the user clicks "Change Theme" in settings.
type ShowThemeEditor struct{}

// ThemeResult is sent when the theme dialog is closed.
type ThemeResult struct {
	Confirmed bool
	Theme     ThemeID
}

// ThemeDialog is a modal dialog for selecting themes.
type ThemeDialog struct {
	visible bool
	width   int
	height  int

	// Theme selection state
	theme         ThemeID
	originalTheme ThemeID
	themeCursor   int
	themes        []Theme

	// For mouse hit detection
	hitRegions        []themeHitRegion
	showKeymapHintsUI bool
}

type themeHitRegion struct {
	index  int
	region HitRegion
}

// NewThemeDialog creates a new theme dialog with current values.
func NewThemeDialog(currentTheme ThemeID) *ThemeDialog {
	themes := AvailableThemes()
	themeCursor := 0
	for i, t := range themes {
		if t.ID == currentTheme {
			themeCursor = i
			break
		}
	}

	return &ThemeDialog{
		theme:         currentTheme,
		originalTheme: currentTheme,
		themes:        themes,
		themeCursor:   themeCursor,
	}
}

func (d *ThemeDialog) Show()                        { d.visible = true; d.originalTheme = d.theme }
func (d *ThemeDialog) Hide()                        { d.visible = false }
func (d *ThemeDialog) Visible() bool                { return d.visible }
func (d *ThemeDialog) SetSize(w, h int)             { d.width, d.height = w, h }
func (d *ThemeDialog) SetShowKeymapHints(show bool) { d.showKeymapHintsUI = show }
func (d *ThemeDialog) Cursor() *tea.Cursor          { return nil }

// SetTheme updates the dialog's selected theme (used when reopening dialog).
func (d *ThemeDialog) SetTheme(theme ThemeID) {
	d.theme = theme
	d.originalTheme = theme
	for i, t := range d.themes {
		if t.ID == theme {
			d.themeCursor = i
			break
		}
	}
}

// Update handles input.
func (d *ThemeDialog) Update(msg tea.Msg) (*ThemeDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return d, d.handleClick(msg)
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			d.theme = d.originalTheme
			d.visible = false
			return d, func() tea.Msg {
				return SafeBatch(
					func() tea.Msg { return ThemePreview{Theme: d.originalTheme} },
					func() tea.Msg { return ThemeResult{Confirmed: false} },
				)()
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			d.visible = false
			return d, func() tea.Msg {
				return ThemeResult{
					Confirmed: true,
					Theme:     d.theme,
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			return d.handleNext()

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			return d.handlePrev()
		}
	}

	return d, nil
}

// handleNext cycles to the next theme.
func (d *ThemeDialog) handleNext() (*ThemeDialog, tea.Cmd) {
	d.themeCursor = (d.themeCursor + 1) % len(d.themes)
	d.theme = d.themes[d.themeCursor].ID
	return d, func() tea.Msg { return ThemePreview{Theme: d.theme} }
}

// handlePrev cycles to the previous theme.
func (d *ThemeDialog) handlePrev() (*ThemeDialog, tea.Cmd) {
	d.themeCursor--
	if d.themeCursor < 0 {
		d.themeCursor = len(d.themes) - 1
	}
	d.theme = d.themes[d.themeCursor].ID
	return d, func() tea.Msg { return ThemePreview{Theme: d.theme} }
}

func (d *ThemeDialog) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	lines := d.renderLines()
	contentHeight := len(lines)
	if contentHeight == 0 {
		return nil
	}

	dialogX, dialogY, dialogW, dialogH := d.dialogBounds(contentHeight)
	if msg.X < dialogX || msg.X >= dialogX+dialogW || msg.Y < dialogY || msg.Y >= dialogY+dialogH {
		return nil
	}

	_, _, contentOffsetX, contentOffsetY := d.dialogFrame()
	localX := msg.X - dialogX - contentOffsetX
	localY := msg.Y - dialogY - contentOffsetY
	if localX < 0 || localY < 0 {
		return nil
	}

	for _, hit := range d.hitRegions {
		if hit.region.Contains(localX, localY) {
			d.themeCursor = hit.index
			d.theme = d.themes[d.themeCursor].ID
			return func() tea.Msg { return ThemePreview{Theme: d.theme} }
		}
	}
	return nil
}

func (d *ThemeDialog) View() string {
	if !d.visible {
		return ""
	}
	return d.dialogStyle().Render(strings.Join(d.renderLines(), "\n"))
}

func (d *ThemeDialog) dialogContentWidth() int {
	if d.width > 0 {
		return min(40, max(30, d.width-20))
	}
	return 35
}

func (d *ThemeDialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

func (d *ThemeDialog) dialogFrame() (frameX, frameY, offsetX, offsetY int) {
	frameX, frameY = d.dialogStyle().GetFrameSize()
	return frameX, frameY, frameX / 2, frameY / 2
}

func (d *ThemeDialog) dialogBounds(contentHeight int) (x, y, w, h int) {
	contentWidth := d.dialogContentWidth()
	frameX, frameY, _, _ := d.dialogFrame()
	w, h = contentWidth+frameX, contentHeight+frameY
	x, y = (d.width-w)/2, (d.height-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return
}

func (d *ThemeDialog) addHit(index, y int) {
	d.hitRegions = append(d.hitRegions, themeHitRegion{
		index:  index,
		region: HitRegion{X: 0, Y: y, Width: d.dialogContentWidth(), Height: 1},
	})
}

func (d *ThemeDialog) renderLines() []string {
	d.hitRegions = d.hitRegions[:0]
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	lines = append(lines, title.Render("Select Theme"), "")

	for i, t := range d.themes {
		style, prefix := muted, "  "
		if i == d.themeCursor {
			style = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
			prefix = Icons.Cursor + " "
		}
		y := len(lines)
		lines = append(lines, prefix+style.Render(t.Name))
		d.addHit(i, y)
	}
	lines = append(lines, "")

	style := muted
	lines = append(lines, style.Render("[Enter] Save • [Esc] Cancel"))

	if d.showKeymapHintsUI {
		lines = append(lines, "", muted.Render("↑/↓ navigate"))
	}

	return lines
}
