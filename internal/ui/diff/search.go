package diff

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// diffMatch is one literal-substring hit: a rune span inside a source line's
// stripped Content. Matches are per-span (a line with two hits is two
// matches) so n/N cycling reaches every occurrence, mirroring the output
// viewers' contract.
type diffMatch struct {
	lineIdx            int
	startRune, endRune int
}

// Searching reports whether the query field owns input — used by the center
// wrapper to keep viewer keys out of tab-level handling while typing.
func (m *Model) Searching() bool { return m.searching }

// updateSearchEdit is the query-field input loop: every key edits the query
// and recomputes matches live (selection never jumps until accept), and
// nothing in this mode can close the tab. esc leaves edit mode only (query
// retained); enter accepts. A pasted newline acts as enter — paste then
// accept is one gesture.
func (m *Model) updateSearchEdit(msg tea.Msg) (*Model, tea.Cmd) {
	if pm, ok := msg.(tea.PasteMsg); ok {
		if strings.ContainsAny(pm.Content, "\r\n") {
			m.query += common.PasteFirstLine(pm.Content)
			m.recomputeMatches()
			m.acceptSearch()
			return m, nil
		}
		m.query += common.KeepPrintable(pm.Content)
		m.recomputeMatches()
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		m.searching = false
		return m, nil
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		m.acceptSearch()
		return m, nil
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("backspace"))):
		m.query = common.TrimLastRune(m.query)
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+u"))):
		m.query = ""
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+w"))):
		m.query = common.TrimLastWord(m.query)
	default:
		if keyMsg.Text != "" {
			m.query += common.KeepPrintable(keyMsg.Text)
		} else {
			return m, nil
		}
	}
	m.recomputeMatches()
	return m, nil
}

// acceptSearch leaves edit mode: an empty query clears search state; with
// matches it selects the first and scrolls it (with hunk context) into view;
// with none it keeps the query so `0 matches` shows and n/N stay inert.
func (m *Model) acceptSearch() {
	m.searching = false
	m.searchWrapped = false
	if m.query == "" {
		m.matches = nil
		m.matchIdx = -1
		return
	}
	if len(m.matches) == 0 {
		m.matchIdx = -1
		return
	}
	m.matchIdx = 0
	m.scrollToMatch()
}

// clearSearch drops the query state entirely — the browse-mode esc.
func (m *Model) clearSearch() {
	m.query = ""
	m.matches = nil
	m.matchIdx = -1
	m.searchWrapped = false
}

// jumpMatch advances the match selection by dir, wrapping at the ends and
// marking the jump so the footer can show (wrapped). Inert without a
// selection — whereupon n keeps its hunk-navigation meaning.
func (m *Model) jumpMatch(dir int) {
	if m.matchIdx < 0 || len(m.matches) == 0 {
		return
	}
	next := m.matchIdx + dir
	m.searchWrapped = false
	if next >= len(m.matches) {
		next = 0
		m.searchWrapped = true
	} else if next < 0 {
		next = len(m.matches) - 1
		m.searchWrapped = true
	}
	m.matchIdx = next
	m.scrollToMatch()
}

// matchVisualRow resolves the selected match to the first visual row of its
// source line whose rendered segment intersects the match's rune span — a
// hit in a wrapped tail needs its own segment in view, not the line's first
// row. Only the selected line's contiguous rows are inspected, reusing the
// highlight predicate so match coordinates never become cell widths or ANSI
// bytes. Falls back to the line's first row when no displayed segment
// intersects (a nowrap-truncated tail or a width-safe placeholder row), so
// navigation stays anchored to the source line.
func matchVisualRow(rows *visualRows, sel diffMatch) int {
	first := rows.topFor(sel.lineIdx)
	if sel.lineIdx < 0 || sel.lineIdx >= len(rows.firstRow) {
		return first
	}
	end := len(rows.rows)
	if sel.lineIdx+1 < len(rows.firstRow) {
		end = rows.firstRow[sel.lineIdx+1]
	}
	for i := first; i < end; i++ {
		if lo, _ := rowHighlight(rows.rows[i], sel); lo >= 0 {
			return i
		}
	}
	return first
}

// scrollToMatch positions the viewport on the visual row containing the
// selected match, pulling the enclosing @@ hunk header into view when it
// fits with that row — a bare +foo match without its hunk header is useless
// context, but the header must never crowd out the hit itself.
func (m *Model) scrollToMatch() {
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) || m.diff == nil {
		return
	}
	sel := m.matches[m.matchIdx]
	rows := m.rows()
	matchRow := matchVisualRow(rows, sel)

	// Nearest preceding hunk header — Hunks are ordered by StartLine.
	headerRow := -1
	for _, h := range m.diff.Hunks {
		if h.StartLine <= sel.lineIdx {
			headerRow = rows.topFor(h.StartLine)
		} else {
			break
		}
	}
	height := m.contentHeight()
	if headerRow >= 0 && matchRow-headerRow < height {
		// Header and match fit one viewport: anchor the header at top.
		m.scroll = m.clampScroll(headerRow)
		return
	}
	m.scroll = m.clampScroll(matchRow - height/2)
}

// recomputeMatches scans the diff's source lines for the query — literal,
// case-insensitive substring over the stripped Content (so +/- markers and
// @@ headers match as written). Rune spans are recorded on the stripped
// text, the same coordinates buildVisualRows segments in.
func (m *Model) recomputeMatches() {
	prevLine := -1
	if m.matchIdx >= 0 && m.matchIdx < len(m.matches) {
		prevLine = m.matches[m.matchIdx].lineIdx
	}
	m.matches = m.matches[:0]
	if m.diff == nil || m.query == "" {
		m.matchIdx = -1
		m.searchWrapped = false
		return
	}
	search := common.NewLiteralSearch(m.query)
	for li, line := range m.diff.Lines {
		text := ansi.Strip(line.Content)
		for _, span := range search.RuneSpans(text) {
			m.matches = append(m.matches, diffMatch{lineIdx: li, startRune: span.Start, endRune: span.End})
		}
	}
	// Keep the selection on the same line when possible — live-recompute
	// while typing should not jump the viewport around.
	if len(m.matches) == 0 {
		m.matchIdx = -1
		return
	}
	if prevLine < 0 {
		m.matchIdx = 0
		return
	}
	for i, mt := range m.matches {
		if mt.lineIdx >= prevLine {
			m.matchIdx = i
			return
		}
	}
	m.matchIdx = len(m.matches) - 1
}

// rowHighlight returns the rune span of the row's raw segment that the
// selected match covers, or (-1,-1). The row's content window is
// [segStartRune, segStartRune+segContentRunes) in line coordinates.
func rowHighlight(row visualRow, sel diffMatch) (lo, hi int) {
	if sel.lineIdx != row.lineIdx || row.segContentRunes == 0 {
		return -1, -1
	}
	lo = sel.startRune - row.segStartRune
	hi = sel.endRune - row.segStartRune
	if hi <= 0 || lo >= row.segContentRunes {
		return -1, -1
	}
	if lo < 0 {
		lo = 0
	}
	if hi > row.segContentRunes {
		hi = row.segContentRunes
	}
	return lo, hi
}

// renderRowMatch re-styles one row with the selected match's rune span in
// reverse video — the output viewers' highlight contract. Gutter and line
// kind styling are preserved; only the matched span inverts.
func (m *Model) renderRowMatch(row visualRow, sel diffMatch) string {
	lo, hi := rowHighlight(row, sel)
	if lo < 0 {
		return row.text
	}
	line := m.diff.Lines[row.lineIdx]
	style := lineContentStyle(line.Kind)
	runes := []rune(row.raw)
	if hi > len(runes) {
		hi = len(runes)
	}
	gutter := strings.Repeat(" ", m.lineNumWidth())
	if row.segIdx == 0 {
		gutter = lipgloss.NewStyle().
			Foreground(common.ColorMuted()).
			Width(m.lineNumWidth()).
			Align(lipgloss.Right).
			Render(strconv.Itoa(row.lineIdx + 1))
	}
	return gutter + " " +
		style.Render(string(runes[:lo])) +
		style.Reverse(true).Render(string(runes[lo:hi])) +
		style.Render(string(runes[hi:]))
}
