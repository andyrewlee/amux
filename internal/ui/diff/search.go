package diff

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
			m.query += pasteFirstLine(pm.Content)
			m.recomputeMatches()
			m.acceptSearch()
			return m, nil
		}
		m.query += keepPrintable(pm.Content)
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
		m.query = trimLastRune(m.query)
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+u"))):
		m.query = ""
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+w"))):
		m.query = trimLastWord(m.query)
	default:
		if keyMsg.Text != "" {
			m.query += keepPrintable(keyMsg.Text)
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

// scrollToMatch positions the viewport on the selected match's first visual
// row, pulling the enclosing @@ hunk header into view when it fits — a bare
// +foo match without its hunk header is useless context.
func (m *Model) scrollToMatch() {
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) || m.diff == nil {
		return
	}
	rows := m.rows()
	matchRow := rows.topFor(m.matches[m.matchIdx].lineIdx)

	// Nearest preceding hunk header — Hunks are ordered by StartLine.
	headerRow := -1
	for _, h := range m.diff.Hunks {
		if h.StartLine <= m.matches[m.matchIdx].lineIdx {
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
	lq := strings.ToLower(m.query)
	for li, line := range m.diff.Lines {
		text := ansi.Strip(line.Content)
		ll := strings.ToLower(text)
		for off := 0; off+len(lq) <= len(ll); {
			i := strings.Index(ll[off:], lq)
			if i < 0 {
				break
			}
			byteStart := off + i
			byteEnd := byteStart + len(lq)
			startRune := utf8.RuneCountInString(text[:byteStart])
			endRune := startRune + utf8.RuneCountInString(text[byteStart:byteEnd])
			m.matches = append(m.matches, diffMatch{lineIdx: li, startRune: startRune, endRune: endRune})
			off = byteEnd // non-overlapping matches only
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

// --- Field-editing helpers, mirrored from internal/ui/common (the output
// viewers' search field runs the same byte/rune policy; the helpers are
// private there and not worth exporting for one consumer — see the plan's
// mirror-not-extract note).

// pasteFirstLine mirrors the output viewers' paste semantics — a newline in
// pasted text ends the query at the first line (enter-accept follows).
func pasteFirstLine(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	line, _, _ := strings.Cut(content, "\n")
	return keepPrintable(line)
}

func keepPrintable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsGraphic(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(r[:len(r)-1])
}

// trimLastWord drops a trailing run of spaces then the preceding word —
// ctrl+w behavior in the query field.
func trimLastWord(s string) string {
	rs := []rune(s)
	i := len(rs)
	for i > 0 && unicode.IsSpace(rs[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(rs[i-1]) {
		i--
	}
	return string(rs[:i])
}
