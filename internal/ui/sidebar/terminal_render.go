package sidebar

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

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

// TerminalOrigin returns the absolute origin for terminal rendering.
func (m *TerminalModel) TerminalOrigin() (int, int) {
	return m.offsetX, m.offsetY
}

// TerminalSize returns the terminal render size.
func (m *TerminalModel) TerminalSize() (int, int) {
	width := m.width
	height := m.height - 1
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

	// Calculate actual terminal dimensions
	termWidth := width
	termHeight := height - 1
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}

	// Resize all terminal vtems only if size changed
	for _, ts := range m.terminals {
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

// formatScrollPos formats scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}
