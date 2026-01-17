package common

import (
	"strings"

	"charm.land/lipgloss/v2"
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

// SetStyles updates the help overlay styles (for theme changes).
func (h *HelpOverlay) SetStyles(styles Styles) {
	h.styles = styles
}

// defaultHelpSections returns the default help sections
func defaultHelpSections() []HelpSection {
	return []HelpSection{
		{
			Title: "Prefix Key (leader key)",
			Bindings: []HelpBinding{
				{"C-Space", "Enter prefix mode"},
				{"C-Space C-Space", "Send literal Ctrl+Space"},
			},
		},
		{
			Title: "After Prefix: Navigation",
			Bindings: []HelpBinding{
				{"h/j/k/l", "Focus pane (←↑↓→)"},
				{"m", "Toggle monitor"},
				{"?", "This help"},
				{"q", "Quit"},
			},
		},
		{
			Title: "After Prefix: Tabs",
			Bindings: []HelpBinding{
				{"a", "Create new agent tab"},
				{"d", "Commit viewer"},
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
				{"D", "Delete worktree / remove project"},
				{"f", "Toggle dirty filter"},
				{"r", "Refresh"},
				{"g/G", "Top/bottom"},
			},
		},
		{
			Title: "Monitor Mode",
			Bindings: []HelpBinding{
				{"hjkl/↑↓←→", "Move selection"},
				{"Enter", "Open selection"},
				{"q/Esc", "Exit monitor"},
			},
		},
		{
			Title: "Copy Mode (center/sidebar terminals)",
			Bindings: []HelpBinding{
				{"q/Esc", "Exit copy mode"},
				{"h/j/k/l or ←↑↓→", "Move cursor"},
				{"PgUp/PgDn", "Scroll half page"},
				{"Ctrl+u/Ctrl+d", "Scroll half page"},
				{"Ctrl+b/Ctrl+f", "Scroll half page"},
				{"g/G", "Top/bottom"},
				{"H/M/L", "Top/middle/bottom of view"},
				{"0/$", "Line start/end"},
				{"w/b/e", "Word forward/back/end"},
				{"/ or ?", "Search forward/back"},
				{"n/N", "Next/prev match"},
				{"Space/v", "Start selection"},
				{"y/Enter", "Copy selection"},
				{"Ctrl+v", "Rectangle toggle"},
			},
		},
		{
			Title: "Dialogs",
			Bindings: []HelpBinding{
				{"Enter", "Confirm"},
				{"Esc", "Cancel"},
				{"Tab/Shift+Tab", "Next/prev option"},
				{"↑/↓", "Move selection"},
			},
		},
		{
			Title: "File Picker",
			Bindings: []HelpBinding{
				{"Enter", "Open/select"},
				{"Esc", "Cancel"},
				{"↑/↓ or Ctrl+n/p", "Move"},
				{"Tab", "Autocomplete"},
				{"/", "Open typed path"},
				{"Backspace", "Up directory"},
				{"Ctrl+h", "Toggle hidden"},
			},
		},
		{
			Title: "Terminal (passthrough)",
			Bindings: []HelpBinding{
				{"PgUp/PgDn", "Scroll in scrollback"},
				{"(all keys)", "Sent to terminal"},
			},
		},
		{
			Title: "Center Pane (direct)",
			Bindings: []HelpBinding{
				{"Ctrl+W", "Close tab"},
				{"Ctrl+S", "Save thread"},
				{"Ctrl+N/P", "Next/prev tab"},
			},
		},
		{
			Title: "Sidebar",
			Bindings: []HelpBinding{
				{"j/k", "Navigate files"},
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
		Render("Press any key or click to close")
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

// WrapHelpItems wraps pre-rendered help items into multiple lines constrained by width.
func WrapHelpItems(items []string, width int) []string {
	if len(items) == 0 {
		return []string{""}
	}
	if width <= 0 {
		return []string{strings.Join(items, "  ")}
	}

	var lines []string
	current := ""
	currentWidth := 0
	sep := "  "
	sepWidth := lipgloss.Width(sep)

	for _, item := range items {
		itemWidth := lipgloss.Width(item)
		if current == "" {
			current = item
			currentWidth = itemWidth
			continue
		}
		if currentWidth+sepWidth+itemWidth <= width {
			current += sep + item
			currentWidth += sepWidth + itemWidth
			continue
		}
		lines = append(lines, current)
		current = item
		currentWidth = itemWidth
	}

	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}
