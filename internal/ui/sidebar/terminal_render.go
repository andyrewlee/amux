package sidebar

import (
	"fmt"

	"charm.land/lipgloss/v2"

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
		// No worktree selected - show non-interactive message
		if m.worktree == nil {
			return m.styles.Muted.Render("No terminal")
		}
		// Worktree selected but no tabs - show clickable "New terminal" button
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

		// Build tab content with close affordance
		closeLabel := m.styles.Muted.Render("×")
		var rendered string
		if i == activeIdx {
			// Active tab - single unified style for clean background
			tabStyle := lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(common.ColorForeground).
				Background(common.ColorSurface2)
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

// TerminalLayer returns a VTermLayer for the active worktree terminal.
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
	showCursor := m.focused && !ts.CopyMode
	if ts.cachedSnap != nil && ts.cachedVersion == version && ts.cachedShowCursor == showCursor {
		return compositor.NewVTermLayer(ts.cachedSnap)
	}

	snap := compositor.NewVTermSnapshotWithCache(ts.VTerm, showCursor, ts.cachedSnap)
	if snap == nil {
		return nil
	}

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
	if ts.CopyMode {
		modeStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorWarning)
		return modeStyle.Render(" COPY MODE (q/Esc exit • j/k/↑/↓ line • PgUp/PgDn/Ctrl+u/d half • g/G top/bottom) ")
	}
	if ts.VTerm.IsScrolled() {
		offset, total := ts.VTerm.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	return ""
}

// HelpLines returns the help lines for the given width, respecting visibility.
func (m *TerminalModel) HelpLines(width int) []string {
	if !m.showKeymapHints {
		return nil
	}
	if width < 1 {
		width = 1
	}
	return m.helpLines(width)
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
			m.helpItem("C-Spc [", "copy"),
			m.helpItem("PgUp", "half up"),
			m.helpItem("PgDn", "half down"),
		)
		if ts.CopyMode {
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

// tabBarHeight is the height of the terminal tab bar (single line, no borders)
const tabBarHeight = 1

// TerminalOrigin returns the absolute origin for terminal rendering.
func (m *TerminalModel) TerminalOrigin() (int, int) {
	// Offset Y by tab bar height when tabs exist
	tabs := m.getTabs()
	if len(tabs) > 0 {
		return m.offsetX, m.offsetY + tabBarHeight
	}
	return m.offsetX, m.offsetY
}

// TerminalSize returns the terminal render size.
func (m *TerminalModel) TerminalSize() (int, int) {
	width := m.width
	height := m.height - 1 // -1 for help bar
	// Subtract tab bar height when tabs exist
	tabs := m.getTabs()
	if len(tabs) > 0 {
		height -= tabBarHeight
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

// SetSize sets the terminal section size
func (m *TerminalModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Calculate actual terminal dimensions (accounting for tab bar and help bar)
	termWidth := width
	termHeight := height - 1 - tabBarHeight // -1 for help bar, -tabBarHeight for tab bar
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}

	// Resize all terminal vtems across all worktrees only if size changed
	for _, tabs := range m.tabsByWorktree {
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
