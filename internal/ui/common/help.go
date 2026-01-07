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
			Title: "Prefix Key (tmux-style)",
			Bindings: []HelpBinding{
				{"C-Space", "Enter prefix mode"},
				{"C-Space C-Space", "Send literal Ctrl+Space"},
			},
		},
		{
			Title: "After Prefix: Navigation",
			Bindings: []HelpBinding{
				{"h/j/k/l", "Focus pane (←↓↑→)"},
				{"g", "Go home (dashboard)"},
				{"m", "Toggle monitor"},
				{"?", "This help"},
				{"q", "Quit"},
			},
		},
		{
			Title: "After Prefix: Tabs",
			Bindings: []HelpBinding{
				{"c", "Create new agent tab"},
				{"x", "Close current tab"},
				{"n/p", "Next/prev tab"},
				{"1-9", "Jump to tab N"},
				{"[", "Enter copy/scroll mode"},
			},
		},
		{
			Title: "Dashboard",
			Bindings: []HelpBinding{
				{"j/k", "Navigate up/down"},
				{"Enter", "Activate worktree"},
				{"D", "Delete worktree"},
				{"f", "Toggle dirty filter"},
				{"r/g", "Refresh"},
			},
		},
		{
			Title: "Terminal (passthrough)",
			Bindings: []HelpBinding{
				{"PgUp/PgDn", "Scroll in scrollback"},
				{"(all keys)", "Sent to terminal"},
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
