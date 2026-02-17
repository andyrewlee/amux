package sidebar

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// renderTabBar renders the terminal tab bar (compact single-line, no borders)
func (m *TerminalModel) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	// Compact tab styles
	inactiveStyle := m.styles.Tab
	plusStyle := m.styles.TabPlus

	if len(tabs) == 0 {
		// No workspace selected - show non-interactive message
		if m.workspace == nil {
			return m.styles.Muted.Render("No terminal")
		}
		// Workspace selected but no tabs - show clickable "New terminal" button
		empty := plusStyle.Render("+ New")
		emptyWidth := lipgloss.Width(empty)
		if emptyWidth > 0 {
			m.tabHits = append(m.tabHits, terminalTabHit{
				kind:  terminalTabHitPlus,
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

	for i, tab := range tabs {
		name := tab.Name
		if name == "" {
			name = fmt.Sprintf("Terminal %d", i+1)
		}
		disconnected := false
		if tab.State != nil {
			tab.State.mu.Lock()
			disconnected = tab.State.Detached || !tab.State.Running
			tab.State.mu.Unlock()
		}

		// Build tab content with close affordance
		closeLabel := m.styles.Muted.Render("×")
		var rendered string
		if i == activeIdx {
			// Active tab - single unified style for clean background
			tabStyle := lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(common.ColorForeground).
				Background(common.ColorSurface2)
			if disconnected {
				tabStyle = tabStyle.Foreground(common.ColorMuted)
			}
			content := name + " ×"
			rendered = tabStyle.Render(content)
		} else {
			// Inactive tab - muted
			nameStyled := m.styles.Muted.Render(name)
			content := nameStyled + " " + closeLabel
			rendered = inactiveStyle.Render(content)
		}

		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth > 0 {
			m.tabHits = append(m.tabHits, terminalTabHit{
				kind:  terminalTabHitTab,
				index: i,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  renderedWidth,
					Height: 1,
				},
			})

			// Close button hit region (padding=1 on each side, no left/right borders)
			padding := 1
			prefixWidth := lipgloss.Width(name + " ")
			closeWidth := lipgloss.Width(closeLabel)
			closeX := x + padding + prefixWidth
			if closeWidth > 0 {
				// Expand close button hit region for easier clicking
				m.tabHits = append(m.tabHits, terminalTabHit{
					kind:  terminalTabHitClose,
					index: i,
					region: common.HitRegion{
						X:      closeX - 1,
						Y:      0,
						Width:  closeWidth + padding + 1,
						Height: 1,
					},
				})
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add (+) button
	btn := plusStyle.Render("+ New")
	btnWidth := lipgloss.Width(btn)
	if btnWidth > 0 {
		m.tabHits = append(m.tabHits, terminalTabHit{
			kind:  terminalTabHitPlus,
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

	return lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
}

// TabBarView returns the rendered tab bar string.
func (m *TerminalModel) TabBarView() string {
	return m.renderTabBar()
}

// TerminalLayer returns a VTermLayer for the active workspace terminal.
func (m *TerminalModel) TerminalLayer() *compositor.VTermLayer {
	ts := m.getTerminal()
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.VTerm == nil {
		return nil
	}

	version := ts.VTerm.Version()
	showCursor := m.focused
	if ts.cachedSnap != nil && ts.cachedVersion == version && ts.cachedShowCursor == showCursor {
		perf.Count("vterm_snapshot_cache_hit", 1)
		return compositor.NewVTermLayer(ts.cachedSnap)
	}

	snap := compositor.NewVTermSnapshotWithCache(ts.VTerm, showCursor, ts.cachedSnap)
	if snap == nil {
		return nil
	}
	perf.Count("vterm_snapshot_cache_miss", 1)

	ts.cachedSnap = snap
	ts.cachedVersion = version
	ts.cachedShowCursor = showCursor
	return compositor.NewVTermLayer(snap)
}

// StatusLine returns the status line for the active terminal.
func (m *TerminalModel) StatusLine() string {
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		return ""
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.VTerm.IsScrolled() {
		offset, total := ts.VTerm.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	if ts.Detached {
		statusStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorWarning)
		return statusStyle.Render(" DETACHED ")
	}
	if !ts.Running {
		statusStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorError)
		return statusStyle.Render(" STOPPED ")
	}
	return ""
}

// HelpLines returns the help lines for the given width, respecting visibility and height.
func (m *TerminalModel) HelpLines(width int) []string {
	return m.helpLinesForLayout(width)
}

func (m *TerminalModel) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *TerminalModel) helpLines(contentWidth int) []string {
	items := []string{}

	ts := m.getTerminal()
	hasTerm := ts != nil && ts.VTerm != nil

	// Tab management hints
	items = append(items, m.helpItem("C-Spc t", "new term"))
	if m.HasMultipleTabs() {
		items = append(items,
			m.helpItem("C-Spc n", "next"),
			m.helpItem("C-Spc p", "prev"),
			m.helpItem("C-Spc x", "close"),
		)
	}
	if hasTerm {
		items = append(items,
			m.helpItem("C-Spc D", "detach"),
			m.helpItem("C-Spc R", "reattach"),
			m.helpItem("C-Spc S", "restart"),
		)
	}

	if hasTerm {
		items = append(items,
			m.helpItem("PgUp", "half up"),
			m.helpItem("PgDn", "half down"),
		)
	}

	return common.WrapHelpItems(items, contentWidth)
}

// tabBarHeight is the height of the terminal tab bar (single line, no borders)
const tabBarHeight = 1

// statusLineReserve keeps space for the status line even when hidden.
const statusLineReserve = 1

func (m *TerminalModel) helpLinesForLayout(width int) []string {
	if !m.showKeymapHints {
		return nil
	}
	if width < 1 {
		width = 1
	}
	helpLines := m.helpLines(width)
	maxHelpHeight := m.height - tabBarHeight - statusLineReserve
	if maxHelpHeight < 0 {
		maxHelpHeight = 0
	}
	if len(helpLines) > maxHelpHeight {
		helpLines = helpLines[:maxHelpHeight]
	}
	return helpLines
}

func (m *TerminalModel) terminalViewportSize() (int, int, []string) {
	width := m.width
	if width < 1 {
		width = 1
	}
	helpLines := m.helpLinesForLayout(width)
	height := m.height - tabBarHeight - statusLineReserve - len(helpLines)
	if height < 1 {
		height = 1
	}
	return width, height, helpLines
}

func (m *TerminalModel) refreshTerminalSize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.SetSize(m.width, m.height)
}

// View renders the terminal section
func (m *TerminalModel) View() string {
	var b strings.Builder
	// Always render tab bar (shows "New terminal" when no tabs exist)
	tabBar := m.renderTabBar()
	if tabBar != "" {
		b.WriteString(tabBar)
		b.WriteString("\n")
	}
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		// Show placeholder when no terminal
		if len(m.getTabs()) == 0 {
			// Empty state - tab bar already shows "New terminal" button
		} else {
			placeholder := m.styles.Muted.Render("No terminal")
			b.WriteString(placeholder)
		}
	} else {
		ts.mu.Lock()
		ts.VTerm.ShowCursor = m.focused
		// Use VTerm.Render() directly - it uses dirty line caching and delta styles
		content := ts.VTerm.Render()
		isScrolled := ts.VTerm.IsScrolled()
		var scrollInfo string
		if isScrolled {
			offset, total := ts.VTerm.GetScrollInfo()
			scrollInfo = formatScrollPos(offset, total)
		}
		ts.mu.Unlock()

		b.WriteString(content)

		if isScrolled {
			b.WriteString("\n")
			scrollStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(common.ColorBackground).
				Background(common.ColorInfo)
			b.WriteString(scrollStyle.Render(" SCROLL: " + scrollInfo + " "))
		}
	}

	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLinesForLayout(contentWidth)

	// Pad to fill height
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - len(helpLines) // Account for help
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(strings.Join(helpLines, "\n"))
	// Ensure output doesn't exceed m.height lines
	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}

// TerminalOrigin returns the absolute origin for terminal rendering.
func (m *TerminalModel) TerminalOrigin() (int, int) {
	// Offset Y by tab bar height (tab bar always renders when terminal exists).
	return m.offsetX, m.offsetY + tabBarHeight
}

// TerminalSize returns the terminal render size.
func (m *TerminalModel) TerminalSize() (int, int) {
	width, height, _ := m.terminalViewportSize()
	return width, height
}

// SetSize sets the terminal section size
func (m *TerminalModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Calculate actual terminal dimensions (accounting for tab bar, help lines, status reserve)
	termWidth, termHeight := m.terminalContentSize()

	// Resize all terminal vtems across all workspaces only if size changed
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.State == nil {
				continue
			}
			ts := tab.State
			ts.mu.Lock()
			if ts.VTerm != nil && (ts.lastWidth != termWidth || ts.lastHeight != termHeight) {
				ts.lastWidth = termWidth
				ts.lastHeight = termHeight
				ts.VTerm.Resize(termWidth, termHeight)
				if ts.Terminal != nil {
					_ = ts.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
				}
			}
			ts.mu.Unlock()
		}
	}
}

// formatScrollPos formats scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}
