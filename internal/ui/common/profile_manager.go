package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/messages"
)

// ShowProfileManager is sent when the user clicks "Manage Profiles" in settings.
type ShowProfileManager struct{}

// ProfileManagerResult is sent when the profile manager is closed.
type ProfileManagerResult struct {
	// No fields needed - actions are sent as separate messages
}

// ProfileManager is a dialog for managing (renaming/deleting) profiles.
type ProfileManager struct {
	visible  bool
	width    int
	height   int
	profiles []string

	cursor          int
	showKeymapHints bool
}

// NewProfileManager creates a new profile manager dialog.
func NewProfileManager(profiles []string) *ProfileManager {
	return &ProfileManager{
		profiles: profiles,
		cursor:   0,
	}
}

func (p *ProfileManager) Show()                        { p.visible = true }
func (p *ProfileManager) Hide()                        { p.visible = false }
func (p *ProfileManager) Visible() bool                { return p.visible }
func (p *ProfileManager) SetSize(w, h int)             { p.width, p.height = w, h }
func (p *ProfileManager) SetShowKeymapHints(show bool) { p.showKeymapHints = show }

// Update handles input.
func (p *ProfileManager) Update(msg tea.Msg) (*ProfileManager, tea.Cmd) {
	if !p.visible {
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			p.visible = false
			return p, func() tea.Msg { return ProfileManagerResult{} }

		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			if len(p.profiles) > 0 {
				p.cursor = (p.cursor + 1) % len(p.profiles)
			}
			return p, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			if len(p.profiles) > 0 {
				p.cursor--
				if p.cursor < 0 {
					p.cursor = len(p.profiles) - 1
				}
			}
			return p, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			// Rename selected profile
			if len(p.profiles) > 0 && p.cursor < len(p.profiles) {
				profile := p.profiles[p.cursor]
				p.visible = false
				return p, func() tea.Msg {
					return messages.ShowRenameProfileDialog{Profile: profile}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("d", "backspace", "delete"))):
			// Delete selected profile
			if len(p.profiles) > 0 && p.cursor < len(p.profiles) {
				profile := p.profiles[p.cursor]
				p.visible = false
				return p, func() tea.Msg {
					return messages.ShowDeleteProfileDialog{Profile: profile}
				}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			// Add new profile
			p.visible = false
			return p, func() tea.Msg {
				return messages.ShowCreateProfileDialog{}
			}
		}
	}

	return p, nil
}

func (p *ProfileManager) View() string {
	if !p.visible {
		return ""
	}
	return p.dialogStyle().Render(strings.Join(p.renderLines(), "\n"))
}

func (p *ProfileManager) dialogContentWidth() int {
	if p.width > 0 {
		return min(50, max(35, p.width-20))
	}
	return 40
}

func (p *ProfileManager) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(p.dialogContentWidth())
}

func (p *ProfileManager) renderLines() []string {
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)
	normal := lipgloss.NewStyle().Foreground(ColorForeground)
	selected := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)

	lines = append(lines, title.Render("Manage Profiles"), "")

	if len(p.profiles) == 0 {
		lines = append(lines, muted.Render("No profiles found."))
		lines = append(lines, "")
		lines = append(lines, muted.Render("Profiles are created when you set"))
		lines = append(lines, muted.Render("a profile on a project."))
	} else {
		for i, profile := range p.profiles {
			prefix := "  "
			style := normal
			if i == p.cursor {
				prefix = "> "
				style = selected
			}
			lines = append(lines, style.Render(prefix+profile))
		}
	}

	lines = append(lines, "")

	// Commands at bottom (like permissions editor)
	if len(p.profiles) > 0 {
		lines = append(lines, muted.Render("[a] Add [r] Rename [d] Delete"))
	} else {
		lines = append(lines, muted.Render("[a] Add"))
	}
	lines = append(lines, muted.Render("[Esc] Close"))

	if p.showKeymapHints && len(p.profiles) > 0 {
		lines = append(lines, "", muted.Render("↑/↓ navigate"))
	}

	return lines
}
