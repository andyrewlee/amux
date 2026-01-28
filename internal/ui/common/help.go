package common

import (
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
			},
		},
		{
			Title: "Dashboard",
			Bindings: []HelpBinding{
				{"j/k", "Navigate up/down"},
				{"Enter", "Activate workspace"},
				{"D", "Delete workspace / remove project"},
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
		_ = cmd.Run() // Run waits for completion, avoiding zombie processes
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
