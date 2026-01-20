package center

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
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
		if tab.CommitViewer != nil {
			// Sync focus state with center pane focus
			tab.CommitViewer.SetFocused(m.focused)
			// Render commit viewer
			b.WriteString(tab.CommitViewer.View())
		} else if tab.Terminal != nil {
			tab.Terminal.ShowCursor = m.focused && !tab.CopyMode
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
	if m.worktree != nil {
		items = append(items,
			m.helpItem("C-Spc a", "new tab"),
			m.helpItem("C-Spc d", "commits"),
		)
	}
	if hasTabs {
		items = append(items,
			m.helpItem("C-Spc s", "save"),
			m.helpItem("C-Spc x", "close"),
			m.helpItem("C-Spc p", "prev"),
			m.helpItem("C-Spc n", "next"),
			m.helpItem("C-Spc 1-9", "jump tab"),
			m.helpItem("C-Spc [", "copy"),
			m.helpItem("PgUp", "scroll up"),
			m.helpItem("PgDn", "scroll down"),
		)
		if m.CopyModeActive() {
			items = append(items,
				m.helpItem("g", "top"),
				m.helpItem("G", "bottom"),
				m.helpItem("Space/v", "select"),
				m.helpItem("y/Enter", "copy"),
				m.helpItem("C-v", "rect"),
				m.helpItem("/?", "search"),
				m.helpItem("n/N", "next/prev"),
				m.helpItem("w/b/e", "word"),
				m.helpItem("H/M/L", "top/mid/bot"),
			)
		}
	}
	return common.WrapHelpItems(items, contentWidth)
}

// renderTabBar renders the tab bar with activity indicators
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(currentTabs) == 0 {
		empty := m.styles.TabPlus.Render("New agent")
		emptyWidth := lipgloss.Width(empty)
		if emptyWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitPlus,
				index: -1,
				region: common.HitRegion{
					X:      0,
					Y:      0,
					Width:  emptyWidth,
					Height: 1,
				},
			})
		}
		return empty
	}

	var renderedTabs []string
	x := 0

	for i, tab := range currentTabs {
		name := tab.Name
		if name == "" {
			name = tab.Assistant
		}

		// Add status indicator
		var indicator string
		if tab.Running {
			indicator = common.Icons.Running + " "
		} else {
			indicator = common.Icons.Idle + " "
		}

		// Get agent-specific color
		var agentStyle lipgloss.Style
		switch tab.Assistant {
		case "claude":
			agentStyle = m.styles.AgentClaude
		case "codex":
			agentStyle = m.styles.AgentCodex
		case "gemini":
			agentStyle = m.styles.AgentGemini
		case "amp":
			agentStyle = m.styles.AgentAmp
		case "opencode":
			agentStyle = m.styles.AgentOpencode
		case "droid":
			agentStyle = m.styles.AgentDroid
		case "cursor":
			agentStyle = m.styles.AgentCursor
		default:
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content with agent-colored indicator and a close affordance
		closeLabel := m.styles.Muted.Render("x")
		content := agentStyle.Render(indicator) + name + " " + closeLabel

		style := m.styles.Tab
		var rendered string
		if i == activeIdx {
			// Active tab gets highlight border
			style = m.styles.ActiveTab
			rendered = style.Render(content)
		} else {
			// Inactive tab
			rendered = style.Render(content)
		}
		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitTab,
				index: i,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  renderedWidth,
					Height: 1,
				},
			})

			frameX, _ := style.GetFrameSize()
			leftFrame := frameX / 2
			prefixWidth := lipgloss.Width(agentStyle.Render(indicator) + name + " ")
			closeWidth := lipgloss.Width(closeLabel)
			closeX := x + leftFrame + prefixWidth
			if closeWidth > 0 {
				// Expand close button hit region for easier clicking
				// Include the space before "x" and extend to end of tab
				expandedCloseX := closeX - 1 // include the space before "x"
				expandedCloseWidth := renderedWidth - leftFrame - prefixWidth + 1
				m.tabHits = append(m.tabHits, tabHit{
					kind:  tabHitClose,
					index: i,
					region: common.HitRegion{
						X:      expandedCloseX,
						Y:      0,
						Width:  expandedCloseWidth,
						Height: 1,
					},
				})
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add control buttons with matching border style
	btn := m.styles.TabPlus.Render("+")
	btnWidth := lipgloss.Width(btn)
	if btnWidth > 0 {
		m.tabHits = append(m.tabHits, tabHit{
			kind:  tabHitPlus,
			index: -1,
			region: common.HitRegion{
				X:      x,
				Y:      0,
				Width:  btnWidth,
				Height: 1,
			},
		})
	}
	renderedTabs = append(renderedTabs, btn)

	// Join tabs horizontally at the bottom so borders align
	return lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
}

func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	// Tab bar is at screen Y=2: Y=0 is pane border, Y=1 is tab border, Y=2 is tab content
	// Account for border (1) and padding (1) on the left side when converting X coordinates
	const (
		borderTop   = 2
		borderLeft  = 1
		paddingLeft = 1
	)
	if msg.Y != borderTop {
		return nil
	}
	// Convert screen X to content X (subtract pane offset, border, and padding)
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	// Convert screen Y to local Y within tab bar content (all tab hits are at Y=0)
	localY := msg.Y - borderTop
	// Check close buttons first (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			return m.closeTabAt(hit.index)
		}
	}
	// Then check tabs and other buttons
	for _, hit := range m.tabHits {
		if hit.region.Contains(localX, localY) {
			switch hit.kind {
			case tabHitPlus:
				return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
			case tabHitTab:
				m.setActiveTabIdx(hit.index)
				return nil
			}
		}
	}
	return nil
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
	b.WriteString("  ")

	// Commits button
	commitsBtn := m.styles.TabPlus.Render("Commits")
	b.WriteString(commitsBtn)

	// Help text
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	b.WriteString(helpStyle.Render("C-Spc a:new agent  C-Spc d:commits"))

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
	if tab.CopyMode {
		modeStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorWarning)
		return modeStyle.Render(" COPY MODE (q/Esc exit • j/k/↑/↓ line • PgUp/PgDn/Ctrl+u/d half • g/G top/bottom) ")
	}
	if tab.Terminal.IsScrolled() {
		offset, total := tab.Terminal.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	return ""
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
