package common

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Search machinery for OutputDialog — the `/` query field's edit loop,
// match bookkeeping, and selection repair/highlight. The dialog struct and
// rendering live in output_dialog.go.

// outputDialogMatch is one literal-substring hit as a rune span inside a
// sanitized line — rune offsets, not bytes, so the highlight renderer can
// slice the displayed runes directly.
type outputDialogMatch struct {
	line               int
	startRune, endRune int
}

// outputDialogMaxLineRunes bounds a single content line — generous; the
// dialog's ~90-column width clips visually anyway.
const outputDialogMaxLineRunes = 512

// updateQueryEdit is the query-field input loop: every key edits the query
// (recomputing matches live — the selection never jumps until accept), and
// nothing in this mode can close the dialog. esc leaves edit mode only
// (query retained); enter accepts. A pasted newline acts as enter — paste
// followed by accept is one gesture.
func (d *OutputDialog) updateQueryEdit(msg tea.Msg) (*OutputDialog, tea.Cmd) {
	if pm, ok := msg.(tea.PasteMsg); ok {
		if strings.ContainsAny(pm.Content, "\r\n") {
			d.query += PasteFirstLine(pm.Content)
			d.recomputeMatches()
			d.acceptSearch()
			return d, nil
		}
		d.query += keepRunes(pm.Content, isPrintableFieldRune)
		d.recomputeMatches()
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch {
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("esc"))):
		d.editing = false
		return d, nil
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("enter"))):
		d.acceptSearch()
		return d, nil
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("backspace"))):
		d.query = TrimLastRune(d.query)
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+u"))):
		d.query = ""
	case key.Matches(keyMsg, key.NewBinding(key.WithKeys("ctrl+w"))):
		d.query = TrimLastWord(d.query)
	default:
		if keyMsg.Text != "" {
			d.query += keepRunes(keyMsg.Text, isPrintableFieldRune)
		} else {
			return d, nil
		}
	}
	d.recomputeMatches()
	return d, nil
}

// acceptSearch leaves edit mode: an empty query clears search state
// entirely; with matches it selects the first and centers it (a manual
// scroll, so follow disengages); with none it keeps the query so `0
// matches` shows in the footer and n/N stay inert.
func (d *OutputDialog) acceptSearch() {
	d.editing = false
	d.wrapped = false
	if d.query == "" {
		d.matches = nil
		d.matchIdx = -1
		return
	}
	if len(d.matches) == 0 {
		d.matchIdx = -1
		return
	}
	d.matchIdx = 0
	d.following = false
	d.centerOnMatch()
}

// jumpMatch advances the match selection by dir, wrapping at the ends and
// marking the jump so the footer can show (wrapped) for it. When a refresh
// cleared the selection (repairMatchSelection drops to -1 once no match
// sits at-or-after the old line) but matches still exist, n re-selects the
// first match and N the last — the cleared state is recoverable, not dead.
func (d *OutputDialog) jumpMatch(dir int) {
	if len(d.matches) == 0 {
		return
	}
	next := d.matchIdx + dir
	d.wrapped = false
	switch {
	case d.matchIdx < 0:
		if dir > 0 {
			next = 0
		} else {
			next = len(d.matches) - 1
		}
	case next >= len(d.matches):
		next = 0
		d.wrapped = true
	case next < 0:
		next = len(d.matches) - 1
		d.wrapped = true
	}
	d.matchIdx = next
	d.following = false
	d.centerOnMatch()
}

func (d *OutputDialog) centerOnMatch() {
	if d.matchIdx < 0 || d.matchIdx >= len(d.matches) {
		return
	}
	d.offset = d.matches[d.matchIdx].line - d.viewCap/2
	d.clampOffset()
}

// recomputeMatches scans the current sanitized snapshot for the query —
// literal, case-insensitive substring over d.lines only (dropped history is
// never searchable). Case folding is strings.ToLower per rune on both
// sides; NewLiteralSearch maps folded match offsets back to rune spans on
// the original line so the highlight slices render runes directly.
func (d *OutputDialog) recomputeMatches() {
	prevLine := -1
	if d.matchIdx >= 0 && d.matchIdx < len(d.matches) {
		prevLine = d.matches[d.matchIdx].line
	}
	d.matches = d.matches[:0]
	if d.query == "" {
		d.matchIdx = -1
		d.wrapped = false
		return
	}
	search := NewLiteralSearch(d.query)
	for li, line := range d.lines {
		for _, span := range search.RuneSpans(line) {
			d.matches = append(d.matches, outputDialogMatch{line: li, startRune: span.Start, endRune: span.End})
		}
	}
	d.repairMatchSelection(prevLine)
}

// repairMatchSelection keeps the selection pointing at the same line after
// a recompute when possible, else the first match at-or-after it (nearest
// surviving match), else -1.
func (d *OutputDialog) repairMatchSelection(prevLine int) {
	if len(d.matches) == 0 {
		d.matchIdx = -1
		return
	}
	if prevLine < 0 {
		d.matchIdx = 0
		return
	}
	for i, m := range d.matches {
		if m.line == prevLine {
			d.matchIdx = i
			return
		}
	}
	for i, m := range d.matches {
		if m.line > prevLine {
			d.matchIdx = i
			return
		}
	}
	d.matchIdx = -1
}

// highlightedLine renders line with the selected match's rune span in
// reverse video. Only the selected match is highlighted — full-buffer
// multi-match highlighting is intentionally deferred (scope).
func highlightedLine(line string, m outputDialogMatch) string {
	rs := []rune(line)
	if m.startRune < 0 || m.endRune > len(rs) || m.startRune >= m.endRune {
		return line
	}
	hl := lipgloss.NewStyle().Reverse(true)
	return string(rs[:m.startRune]) + hl.Render(string(rs[m.startRune:m.endRune])) + string(rs[m.endRune:])
}
