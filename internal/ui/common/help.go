package common

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpSection represents a group of keybindings
type HelpSection struct {
	Title    string
	Bindings []HelpBinding
}

// HelpBinding represents a single keybinding
type HelpBinding struct {
	Key  string
	Desc string
}

// HelpOverlay manages the help overlay display
type HelpOverlay struct {
	visible  bool
	width    int
	height   int
	styles   Styles
	sections []HelpSection
}

// NewHelpOverlay creates a new help overlay
func NewHelpOverlay() *HelpOverlay {
	return &HelpOverlay{
		styles:   DefaultStyles(),
		sections: defaultHelpSections(),
	}
}

// defaultHelpSections returns the default help sections
func defaultHelpSections() []HelpSection {
	return []HelpSection{
		{
			Title: "Navigation",
			Bindings: []HelpBinding{
				{"Ctrl+H", "Focus dashboard"},
				{"Ctrl+L", "Focus center pane"},
				{"Ctrl+;", "Focus sidebar"},
				{"Esc", "Return to dashboard"},
			},
		},
		{
			Title: "Dashboard",
			Bindings: []HelpBinding{
				{"j/k", "Navigate up/down"},
				{"Enter", "Activate worktree"},
				{"n", "New worktree"},
				{"d", "Delete worktree"},
				{"f", "Toggle dirty filter"},
				{"m", "Monitor tabs"},
				{"r", "Refresh"},
			},
		},
		{
			Title: "Center Pane",
			Bindings: []HelpBinding{
				{"Ctrl+T", "New agent tab"},
				{"Ctrl+W", "Close tab"},
				{"Ctrl+]", "Next tab"},
				{"Ctrl+C", "Interrupt agent"},
			},
		},
		{
			Title: "Sidebar",
			Bindings: []HelpBinding{
				{"1/2", "Switch tabs"},
				{"j/k", "Navigate files"},
				{"Enter", "Toggle/select"},
				{"g", "Refresh status"},
			},
		},
	}
}

// Show shows the help overlay
func (h *HelpOverlay) Show() {
	h.visible = true
}

// Hide hides the help overlay
func (h *HelpOverlay) Hide() {
	h.visible = false
}

// Toggle toggles the help overlay visibility
func (h *HelpOverlay) Toggle() {
	h.visible = !h.visible
}

// Visible returns whether the help overlay is visible
func (h *HelpOverlay) Visible() bool {
	return h.visible
}

// SetSize sets the overlay size
func (h *HelpOverlay) SetSize(width, height int) {
	h.width = width
	h.height = height
}

// View renders the help overlay
func (h *HelpOverlay) View() string {
	if !h.visible {
		return ""
	}

	var b strings.Builder

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Render("Help")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Sections
	for _, section := range h.sections {
		// Section header
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorMuted).
			MarginTop(1).
			Render(section.Title)
		b.WriteString(header)
		b.WriteString("\n")

		// Bindings
		for _, binding := range section.Bindings {
			key := lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Width(12).
				Render(binding.Key)
			desc := lipgloss.NewStyle().
				Foreground(ColorForeground).
				Render(binding.Desc)
			b.WriteString("  " + key + desc + "\n")
		}
	}

	// Footer
	b.WriteString("\n")
	footer := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true).
		Render("Press any key to close")
	b.WriteString(footer)

	// Create the overlay box
	boxWidth := 50
	if h.width > 0 && boxWidth > h.width-10 {
		boxWidth = h.width - 10
	}

	content := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused).
		Padding(1, 2).
		Width(boxWidth).
		Render(b.String())

	// Center the overlay
	return lipgloss.Place(
		h.width, h.height,
		lipgloss.Center, lipgloss.Center,
		content,
	)
}

// RenderHelpItem renders a single help item for inline help bars
func RenderHelpItem(styles Styles, key, desc string) string {
	return styles.HelpKey.Render(key) + styles.HelpDesc.Render(":"+desc)
}

// RenderHelpBarItems renders multiple help items for an inline help bar
func RenderHelpBarItems(styles Styles, items []HelpBinding) string {
	var parts []string
	for _, item := range items {
		parts = append(parts, RenderHelpItem(styles, item.Key, item.Desc))
	}
	return strings.Join(parts, "  ")
}
