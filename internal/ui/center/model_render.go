package center

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// formatScrollPos formats the scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}

// View renders the center pane
func (m *Model) View() string {
	defer perf.Time("center_view")()
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Content
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 {
		b.WriteString(m.renderEmpty())
	} else if activeIdx < len(tabs) {
		tab := tabs[activeIdx]
		tab.mu.Lock()
		if tab.DiffViewer != nil {
			// Sync focus state with center pane focus
			tab.DiffViewer.SetFocused(m.focused)
			// Render native diff viewer
			b.WriteString(tab.DiffViewer.View())
		} else if tab.Terminal != nil {
			tab.Terminal.ShowCursor = m.focused
			// Use VTerm.Render() directly - it uses dirty line caching and delta styles
			b.WriteString(tab.Terminal.Render())

			if status := m.terminalStatusLineLocked(tab); status != "" {
				b.WriteString("\n" + status)
			}
		}
		tab.mu.Unlock()
	}

	// Help bar with styled keys (prefix mode)
	contentWidth := m.contentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	// Pad to the inner pane height (border excluded), reserving the help lines.
	// buildBorderedPane will use contentHeight = height - 2, so we target that.
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}

	// Build content with help at bottom
	content := b.String()
	helpContent := strings.Join(helpLines, "\n")

	// Count current lines
	contentLines := strings.Split(content, "\n")
	helpLineCount := len(helpLines)

	// Calculate padding needed
	targetContentLines := innerHeight - helpLineCount
	if targetContentLines < 0 {
		targetContentLines = 0
	}

	// Pad or truncate content to targetContentLines
	if len(contentLines) < targetContentLines {
		// Pad with empty lines
		for len(contentLines) < targetContentLines {
			contentLines = append(contentLines, "")
		}
	} else if len(contentLines) > targetContentLines {
		// Truncate
		contentLines = contentLines[:targetContentLines]
	}

	// Combine content and help
	result := strings.Join(contentLines, "\n")
	if helpContent != "" {
		result += "\n" + helpContent
	}

	return result
}

// TabBarView returns the rendered tab bar string.
func (m *Model) TabBarView() string {
	return m.renderTabBar()
}

// HelpLines returns the help lines for the given width, respecting visibility.
func (m *Model) HelpLines(width int) []string {
	if !m.showKeymapHints {
		return nil
	}
	if width < 1 {
		width = 1
	}
	return m.helpLines(width)
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{}

	hasTabs := len(m.getTabs()) > 0
	if m.workspace != nil {
		items = append(items,
			m.helpItem("C-Spc a", "new agent tab"),
		)
	}
	if hasTabs {
		items = append(items,
			m.helpItem("C-Spc x", "close"),
			m.helpItem("C-Spc D", "detach"),
			m.helpItem("C-Spc R", "reattach"),
			m.helpItem("C-Spc S", "restart"),
			m.helpItem("C-Spc p", "prev"),
			m.helpItem("C-Spc n", "next"),
			m.helpItem("C-Spc 1-9", "jump tab"),
			m.helpItem("PgUp", "scroll up"),
			m.helpItem("PgDn", "scroll down"),
		)
	}
	return common.WrapHelpItems(items, contentWidth)
}

// renderEmpty renders the empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(m.styles.Title.Render("No agents running"))
	b.WriteString("\n\n")

	// New agent button
	agentBtn := m.styles.TabPlus.Render("New agent")
	b.WriteString(agentBtn)

	// Help text
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(common.ColorMuted())
	b.WriteString(helpStyle.Render("C-Spc a:new agent"))

	return b.String()
}

// TerminalViewport returns the terminal content area coordinates relative to the pane.
// Returns (x, y, width, height) where the terminal content should be rendered.
// This is for layer-based rendering positioning within the bordered pane.
// Uses terminalMetrics() as the single source of truth for geometry.
func (m *Model) TerminalViewport() (x, y, width, height int) {
	tm := m.terminalMetrics()
	return tm.ContentStartX, tm.ContentStartY, tm.Width, tm.Height
}

// ViewChromeOnly renders only the pane chrome (border, tab bar, help lines) without
// the terminal content. This is used with VTermLayer for layer-based rendering.
// IMPORTANT: The output structure must match View() exactly so buildBorderedPane
// produces the same layout.
func (m *Model) ViewChromeOnly() string {
	defer perf.Time("center_view_chrome")()
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Calculate content dimensions to match View() exactly
	contentWidth := m.contentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}

	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	statusLine := m.activeTerminalStatusLine()

	// Match View()'s padding logic exactly:
	// innerHeight = m.height - 2 (space inside buildBorderedPane)
	// targetContentLines = innerHeight - helpLineCount
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	helpLineCount := len(helpLines)
	targetContentLines := innerHeight - helpLineCount
	if targetContentLines < 0 {
		targetContentLines = 0
	}

	// We already have 1 line (tab bar), so we need targetContentLines - 1 more lines
	emptyLinesNeeded := targetContentLines - 1
	statusLineVisible := statusLine != ""
	if statusLineVisible {
		if emptyLinesNeeded > 0 {
			emptyLinesNeeded--
		} else {
			statusLineVisible = false
		}
	}
	if emptyLinesNeeded < 0 {
		emptyLinesNeeded = 0
	}

	// Fill with empty lines (will be overwritten by VTermLayer)
	emptyLine := strings.Repeat(" ", contentWidth)
	for i := 0; i < emptyLinesNeeded; i++ {
		b.WriteString(emptyLine)
		b.WriteString("\n")
	}

	if statusLineVisible {
		b.WriteString(statusLine)
		if helpLineCount > 0 {
			b.WriteString("\n")
		}
	}

	// Add help lines at bottom (matching View()'s format)
	helpContent := strings.Join(helpLines, "\n")
	if helpContent != "" {
		b.WriteString(helpContent)
	}

	return b.String()
}

// terminalStatusLineLocked returns the status line for the active terminal.
// Caller must hold tab.mu.
func (m *Model) terminalStatusLineLocked(tab *Tab) string {
	if tab == nil || tab.Terminal == nil {
		return ""
	}
	if tab.Terminal.IsScrolled() {
		offset, total := tab.Terminal.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground()).
			Background(common.ColorInfo())
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	if tab.Running && !tab.Detached {
		return ""
	}
	status := ""
	if tab.Detached {
		status = " DETACHED "
	} else if !tab.Running {
		status = " STOPPED "
	}
	statusStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorBackground()).
		Background(common.ColorInfo())
	if tab.Detached {
		statusStyle = statusStyle.Background(common.ColorWarning())
	} else if !tab.Running {
		statusStyle = statusStyle.Background(common.ColorError())
	}
	return statusStyle.Render(status)
}

// activeTerminalStatusLine returns the status line for the active terminal.
func (m *Model) activeTerminalStatusLine() string {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return ""
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return m.terminalStatusLineLocked(tab)
}

// ActiveTerminalStatusLine returns the status line for the active terminal.
func (m *Model) ActiveTerminalStatusLine() string {
	return m.activeTerminalStatusLine()
}
