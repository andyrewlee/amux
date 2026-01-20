package common

import (
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const docsURL = "https://amux.mintlify.app"

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

	// Navigation state
	selectedSection int
	scrollOffset    int

	// Search state
	searchMode    bool
	searchQuery   string
	searchMatches []int // indices of matching sections
	searchIndex   int   // current match index

	// Cached dialog dimensions for hit testing
	dialogWidth  int
	dialogHeight int

	// Doc link hit region (relative to dialog content)
	docLinkX     int
	docLinkWidth int
	contentLines int // number of content lines (for calculating doc link Y)
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
				{"Type/Enter", "Send input to selected agent"},
				{"C-Space m", "Exit monitor"},
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
				{"↑/↓", "Move"},
				{"Tab", "Autocomplete"},
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

// Show shows the help overlay and resets navigation state
func (h *HelpOverlay) Show() {
	h.visible = true
	h.selectedSection = 0
	h.scrollOffset = 0
	h.resetSearch()
}

// Hide hides the help overlay and resets state
func (h *HelpOverlay) Hide() {
	h.visible = false
	h.selectedSection = 0
	h.scrollOffset = 0
	h.resetSearch()
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

// HelpResult indicates what happened after Update
type HelpResult int

const (
	HelpResultNone   HelpResult = iota // No action needed
	HelpResultClosed                   // Help was closed
)

// Update handles keyboard and mouse input for the help overlay.
// Returns the result and an optional command.
func (h *HelpOverlay) Update(msg tea.Msg) (*HelpOverlay, HelpResult, tea.Cmd) {
	if !h.visible {
		return h, HelpResultNone, nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			if h.isDocLinkClick(msg.X, msg.Y) {
				return h, HelpResultNone, openURL(docsURL)
			}
		}
		return h, HelpResultNone, nil

	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			h.scrollUp()
		} else if msg.Button == tea.MouseWheelDown {
			h.scrollDown()
		}
		return h, HelpResultNone, nil

	case tea.KeyPressMsg:
		// Handle search mode input
		if h.searchMode {
			return h.handleSearchInput(msg)
		}

		// Normal mode keybindings
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "q"))):
			h.Hide()
			return h, HelpResultClosed, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			h.nextSection()
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			h.prevSection()
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			h.selectedSection = 0
			h.scrollOffset = 0
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			h.selectedSection = len(h.sections) - 1
			h.ensureVisible()
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			h.searchMode = true
			h.searchQuery = ""
			h.searchMatches = nil
			h.searchIndex = 0
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			// Next search match
			if len(h.searchMatches) > 0 {
				h.searchIndex = (h.searchIndex + 1) % len(h.searchMatches)
				h.selectedSection = h.searchMatches[h.searchIndex]
				h.ensureVisible()
			}
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("N"))):
			// Previous search match
			if len(h.searchMatches) > 0 {
				h.searchIndex--
				if h.searchIndex < 0 {
					h.searchIndex = len(h.searchMatches) - 1
				}
				h.selectedSection = h.searchMatches[h.searchIndex]
				h.ensureVisible()
			}
			return h, HelpResultNone, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
			// Open documentation in browser
			return h, HelpResultNone, openURL(docsURL)
		}
	}

	return h, HelpResultNone, nil
}

// openURL opens a URL in the default browser
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default: // linux, freebsd, etc.
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}

func (h *HelpOverlay) handleSearchInput(msg tea.KeyPressMsg) (*HelpOverlay, HelpResult, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		h.searchMode = false
		return h, HelpResultNone, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		h.searchMode = false
		h.performSearch()
		if len(h.searchMatches) > 0 {
			h.selectedSection = h.searchMatches[0]
			h.ensureVisible()
		}
		return h, HelpResultNone, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
		if len(h.searchQuery) > 0 {
			h.searchQuery = h.searchQuery[:len(h.searchQuery)-1]
		}
		return h, HelpResultNone, nil

	default:
		// Add character to search query
		if len(msg.Text) > 0 && msg.Text[0] >= 32 && msg.Text[0] < 127 {
			h.searchQuery += msg.Text
		}
		return h, HelpResultNone, nil
	}
}

func (h *HelpOverlay) performSearch() {
	h.searchMatches = nil
	if h.searchQuery == "" {
		return
	}

	query := strings.ToLower(h.searchQuery)
	for i, section := range h.sections {
		if strings.Contains(strings.ToLower(section.Title), query) {
			h.searchMatches = append(h.searchMatches, i)
			continue
		}
		for _, binding := range section.Bindings {
			if strings.Contains(strings.ToLower(binding.Key), query) ||
				strings.Contains(strings.ToLower(binding.Desc), query) {
				h.searchMatches = append(h.searchMatches, i)
				break
			}
		}
	}
	h.searchIndex = 0
}

func (h *HelpOverlay) resetSearch() {
	h.searchMode = false
	h.searchQuery = ""
	h.searchMatches = nil
	h.searchIndex = 0
}

func (h *HelpOverlay) nextSection() {
	if h.selectedSection < len(h.sections)-1 {
		h.selectedSection++
		h.ensureVisible()
	}
}

func (h *HelpOverlay) prevSection() {
	if h.selectedSection > 0 {
		h.selectedSection--
		h.ensureVisible()
	}
}

func (h *HelpOverlay) scrollUp() {
	if h.scrollOffset > 0 {
		h.scrollOffset--
	}
}

func (h *HelpOverlay) scrollDown() {
	maxOffset := len(h.sections) - h.maxVisibleSections()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if h.scrollOffset < maxOffset {
		h.scrollOffset++
	}
}

func (h *HelpOverlay) ensureVisible() {
	// Rough estimate: ensure selected section is visible
	// Each section takes approximately 3-5 lines
	maxVisible := h.maxVisibleSections()
	if h.selectedSection < h.scrollOffset {
		h.scrollOffset = h.selectedSection
	} else if h.selectedSection >= h.scrollOffset+maxVisible {
		h.scrollOffset = h.selectedSection - maxVisible + 1
	}
}

func (h *HelpOverlay) maxVisibleSections() int {
	// Use a fixed max height for the dialog (not the terminal height)
	// Show 4 sections at a time - this keeps the dialog compact
	return 4
}

// View renders the help overlay
func (h *HelpOverlay) View() string {
	if !h.visible {
		return ""
	}

	// Calculate box width (wider layout: 70 chars, responsive)
	boxWidth := 70
	if h.width > 0 && boxWidth > h.width-10 {
		boxWidth = h.width - 10
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	// Build content lines with height constraint
	var lines []string

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Render("Help")
	lines = append(lines, title)
	lines = append(lines, "")

	// Calculate visible range for scrolling
	maxVisible := h.maxVisibleSections()
	startIdx := h.scrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(h.sections) {
		endIdx = len(h.sections)
	}

	// Clamp scroll offset
	if h.scrollOffset > len(h.sections)-maxVisible {
		h.scrollOffset = max(0, len(h.sections)-maxVisible)
	}

	// Scroll indicator at top if not at beginning
	if startIdx > 0 {
		scrollUp := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render("  ↑ more above")
		lines = append(lines, scrollUp)
	}

	// Sections
	for i := startIdx; i < endIdx; i++ {
		section := h.sections[i]
		isSelected := i == h.selectedSection
		isMatch := h.isSearchMatch(i)

		// Section header with selection indicator
		var prefix string
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted)
		if isSelected {
			prefix = Icons.Cursor + " "
			headerStyle = headerStyle.Foreground(ColorPrimary)
		} else {
			prefix = "  "
		}
		if isMatch {
			headerStyle = headerStyle.Background(lipgloss.Color("#3d4f5f"))
		}

		lines = append(lines, prefix+headerStyle.Render(section.Title))

		// Bindings
		keyWidth := 18 // wider for longer key combos
		for _, binding := range section.Bindings {
			keyStyle := lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Width(keyWidth)
			descStyle := lipgloss.NewStyle().
				Foreground(ColorForeground)

			lines = append(lines, "  "+keyStyle.Render(binding.Key)+descStyle.Render(binding.Desc))
		}
		lines = append(lines, "") // blank line between sections
	}

	// Scroll indicator at bottom if not at end
	if endIdx < len(h.sections) {
		scrollDown := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render("  ↓ more below")
		lines = append(lines, scrollDown)
	}

	// Divider
	divider := lipgloss.NewStyle().
		Foreground(ColorBorder).
		Render(strings.Repeat("─", boxWidth-6))
	lines = append(lines, divider)

	// Footer with navigation hints and doc link
	var footerLine string
	if h.searchMode {
		// Search input mode
		searchPrompt := lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Render("/")
		searchText := lipgloss.NewStyle().
			Foreground(ColorForeground).
			Render(h.searchQuery + "█")
		footerLine = searchPrompt + searchText
	} else {
		// Navigation hints on left, clickable doc link on right
		navHints := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render("j/k:section  /:search  Esc:close")

		// Create styled doc link
		docText := "Documentation"
		docLink := lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Underline(true).
			Render(docText)

		// Calculate spacing to right-align doc link
		navWidth := lipgloss.Width(navHints)
		docWidth := len(docText)     // visible width only
		contentWidth := boxWidth - 6 // account for padding
		spacing := contentWidth - navWidth - docWidth
		if spacing < 2 {
			spacing = 2
		}

		// Track doc link position for click detection
		h.docLinkX = navWidth + spacing
		h.docLinkWidth = docWidth

		footerLine = navHints + strings.Repeat(" ", spacing) + docLink
	}
	lines = append(lines, footerLine)

	// Track content lines for click detection
	h.contentLines = len(lines)

	// Join all lines
	content := strings.Join(lines, "\n")

	// Create the overlay box (no lipgloss.Place - centering done by app_view.go)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorderFocused).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	// Cache dimensions for hit testing
	h.dialogWidth = lipgloss.Width(box)
	h.dialogHeight = lipgloss.Height(box)

	return box
}

// ContainsClick returns true if the click coordinates are inside the dialog.
// The x, y coordinates should be absolute screen coordinates.
func (h *HelpOverlay) ContainsClick(x, y int) bool {
	if !h.visible || h.dialogWidth == 0 || h.dialogHeight == 0 {
		return false
	}

	// Calculate dialog position (centered)
	dialogX := (h.width - h.dialogWidth) / 2
	dialogY := (h.height - h.dialogHeight) / 2

	return x >= dialogX && x < dialogX+h.dialogWidth &&
		y >= dialogY && y < dialogY+h.dialogHeight
}

// isDocLinkClick checks if a click is on the documentation link.
func (h *HelpOverlay) isDocLinkClick(x, y int) bool {
	// Don't check for doc link clicks in search mode (link isn't visible)
	if !h.visible || h.searchMode || h.dialogWidth == 0 || h.contentLines == 0 {
		return false
	}

	// Calculate dialog position (centered)
	dialogX := (h.width - h.dialogWidth) / 2
	dialogY := (h.height - h.dialogHeight) / 2

	// Content area starts after border (1) and padding (1)
	contentStartX := dialogX + 1 + 2 // border + left padding
	contentStartY := dialogY + 1 + 1 // border + top padding

	// Footer line is the last line of content
	footerY := contentStartY + h.contentLines - 1

	// Check if click is on the footer line and within doc link X range
	localX := x - contentStartX
	if y == footerY && localX >= h.docLinkX && localX < h.docLinkX+h.docLinkWidth {
		return true
	}

	return false
}

// isSearchMatch returns true if the section index is in search matches
func (h *HelpOverlay) isSearchMatch(idx int) bool {
	for _, m := range h.searchMatches {
		if m == idx {
			return true
		}
	}
	return false
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
