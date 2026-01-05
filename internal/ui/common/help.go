package common

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/andyrewlee/amux/internal/keymap"
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
func NewHelpOverlay(km keymap.KeyMap) *HelpOverlay {
	return &HelpOverlay{
		styles:   DefaultStyles(),
		sections: defaultHelpSections(km),
	}
}

// defaultHelpSections returns the default help sections
func defaultHelpSections(km keymap.KeyMap) []HelpSection {
	leaderHint := keymap.LeaderSequenceHint(km)
	if leaderHint == "" {
		leaderHint = "leader"
	}

	return []HelpSection{
		{
			Title: "Leader",
			Bindings: []HelpBinding{
				{leaderHint, "Command prefix"},
				{keymap.LeaderSequenceHint(km, km.FocusLeft, km.FocusDown, km.FocusUp, km.FocusRight), "Focus panes"},
				{keymap.LeaderSequenceHint(km, km.TabPrev, km.TabNext), "Switch tabs"},
				{keymap.LeaderSequenceHint(km, km.TabNew), "New agent tab"},
				{keymap.LeaderSequenceHint(km, km.TabClose), "Close tab"},
				{keymap.LeaderSequenceHint(km, km.MonitorToggle), "Monitor tabs"},
				{keymap.LeaderSequenceHint(km, km.Home), "Home"},
				{keymap.LeaderSequenceHint(km, km.Help), "Toggle help"},
				{keymap.LeaderSequenceHint(km, km.Quit), "Quit"},
				{keymap.LeaderSequenceHint(km, km.ScrollUpHalf, km.ScrollDownHalf), "Scroll"},
			},
		},
		{
			Title: "Dashboard",
			Bindings: []HelpBinding{
				{keymap.PairHint(km.DashboardDown, km.DashboardUp), "Navigate"},
				{keymap.PairHint(km.DashboardTop, km.DashboardBottom), "Top/bottom"},
				{keymap.PrimaryKey(km.DashboardEnter), "Activate worktree"},
				{keymap.PrimaryKey(km.DashboardNewWorktree), "New worktree"},
				{km.DashboardDelete.Help().Key, "Delete worktree"},
				{keymap.PrimaryKey(km.DashboardToggle), "Toggle dirty filter"},
				{keymap.PrimaryKey(km.DashboardRefresh), "Refresh"},
			},
		},
		{
			Title: "Monitor",
			Bindings: []HelpBinding{
				{keymap.PairHint(km.MonitorLeft, km.MonitorRight), "Move left/right"},
				{keymap.PairHint(km.MonitorUp, km.MonitorDown), "Move up/down"},
				{keymap.PrimaryKey(km.MonitorActivate), "Open selected agent"},
				{keymap.PrimaryKey(km.MonitorExit), "Exit monitor"},
			},
		},
		{
			Title: "Sidebar",
			Bindings: []HelpBinding{
				{keymap.PairHint(km.SidebarDown, km.SidebarUp), "Navigate files"},
				{keymap.PrimaryKey(km.SidebarRefresh), "Refresh status"},
			},
		},
		{
			Title: "Mouse",
			Bindings: []HelpBinding{
				{"Click pane", "Focus pane"},
				{"Click tab", "Switch tabs"},
				{"Right-click tab", "Close tab"},
				{"Click [+]", "New agent tab"},
				{"Click row", "Select/activate"},
				{"Scroll wheel", "Scroll terminal"},
				{"Click monitor tile", "Open agent"},
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
