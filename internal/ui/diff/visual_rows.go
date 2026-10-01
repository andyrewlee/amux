package diff

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// visualRow is a single rendered display row of the diff viewer: the gutter
// (emitted only on a source line's first wrapped segment) plus styled content.
// Line/segment indexes give navigation and resize anchoring a stable mapping
// back to the immutable source lines.
type visualRow struct {
	lineIdx int
	segIdx  int
	text    string
	// raw is the segment's unstyled text and segStart/segRunes its coverage
	// of the source line's content in runes — together they let the render
	// path re-style exactly the selected search match without touching the
	// ANSI bytes buildVisualRows already emitted.
	raw             string
	segStartRune    int
	segContentRunes int
}

// visualRows is the cached display-row layout of an immutable DiffResult.
// Every scrollable position is a visual-row index — the model's scroll offset,
// maxScroll, wheel gating, and hunk jumps all live in this space so wrapped
// tails stay reachable within a bounded viewport.
type visualRows struct {
	rows []visualRow
	// firstRow maps a source-line index to its first visual row, so hunk
	// StartLine navigation survives wrapping.
	firstRow []int
}

// topFor returns the first visual row of a source line; out-of-range line
// indexes (a hunk recorded against a truncated diff) anchor to the top.
func (v *visualRows) topFor(lineIdx int) int {
	if lineIdx < 0 || lineIdx >= len(v.firstRow) {
		return 0
	}
	return v.firstRow[lineIdx]
}

// rowForAnchor returns the visual row for a (line, segment) anchor captured
// before a layout invalidation: the same source line, with the segment
// clamped to however many segments the line now wraps to.
func (v *visualRows) rowForAnchor(lineIdx, segIdx int) int {
	if len(v.rows) == 0 || lineIdx < 0 || lineIdx >= len(v.firstRow) {
		return 0
	}
	next := len(v.rows)
	if lineIdx+1 < len(v.firstRow) {
		next = v.firstRow[lineIdx+1]
	}
	row := v.firstRow[lineIdx] + segIdx
	if row >= next {
		row = next - 1
	}
	return row
}

// anchor returns the (line, segment) identity of the row at the given scroll
// offset — clamped to the last row when the offset sits past the end.
func (v *visualRows) anchor(scroll int) (lineIdx, segIdx int) {
	if len(v.rows) == 0 {
		return 0, 0
	}
	if scroll >= len(v.rows) {
		scroll = len(v.rows) - 1
	}
	if scroll < 0 {
		scroll = 0
	}
	row := v.rows[scroll]
	return row.lineIdx, row.segIdx
}

// visualRowsKey captures every input the row layout reads; scrolling is not
// an input, so ordinary scroll updates never rebuild wrapped rows.
type visualRowsKey struct {
	diff      *git.DiffResult
	width     int
	wrap      bool
	stylesRev uint64
}

// contentWidth is the number of display cells available for line content
// after the gutter and its separator — the real width, not a floor, so narrow
// viewports stay bounded (buildVisualRows degrades wide graphemes to a
// width-safe placeholder rather than emitting a broken half glyph).
func (m *Model) contentWidth() int {
	numWidth := m.lineNumWidth()
	if w := m.width - numWidth - 1; w > 0 {
		return w
	}
	return 1
}

func (m *Model) lineNumWidth() int {
	n := 0
	if m.diff != nil {
		n = len(m.diff.Lines)
	}
	w := len(strconv.Itoa(n))
	if w < 3 {
		w = 3
	}
	return w
}

// visualRows returns the cached row layout, rebuilding only when the diff
// pointer, width, wrap mode, or styles revision changed.
func (m *Model) rows() *visualRows {
	if m.diff == nil {
		return &visualRows{}
	}
	key := visualRowsKey{diff: m.diff, width: m.width, wrap: m.wrap, stylesRev: m.stylesRev}
	if m.rowsValid && m.rowsKey == key {
		return &m.rowsCache
	}
	m.rowsCache = m.buildVisualRows()
	m.rowsKey, m.rowsValid = key, true
	m.rowsBuilt++
	return &m.rowsCache
}

// invalidateRows drops the row layout — callers that replace or clear the
// diff use it so a recycled pointer can never alias a stale layout.
func (m *Model) invalidateRows() {
	m.rowsValid = false
	m.rowsCache = visualRows{}
}

// buildVisualRows renders every source line into its display rows: one
// truncated row per line without wrap, or one row per wrapped segment with
// the gutter on the first segment and blank continuation gutters after.
func (m *Model) buildVisualRows() visualRows {
	lines := m.diff.Lines
	numWidth := m.lineNumWidth()
	contentWidth := m.contentWidth()

	gutterStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Width(numWidth).
		Align(lipgloss.Right)
	blankGutter := strings.Repeat(" ", numWidth)

	out := visualRows{firstRow: make([]int, len(lines))}
	for i, line := range lines {
		out.firstRow[i] = len(out.rows)

		// Diff content is repo-controlled bytes: strip terminal escapes
		// (OSC8 hyperlinks, OSC52 clipboard writes, CSI) before styling so a
		// crafted file can't inject sequences into the host terminal.
		content := ansi.Strip(line.Content)
		style := lineContentStyle(line.Kind)

		var segments []string
		var truncTail string
		switch {
		case m.wrap:
			segments = strings.Split(ansi.Hardwrap(content, contentWidth, false), "\n")
		case ansi.StringWidth(content) > contentWidth:
			tail := "…"
			if contentWidth > 3 {
				tail = "..."
			}
			truncTail = tail
			segments = []string{ansi.Truncate(content, contentWidth, tail)}
		default:
			segments = []string{content}
		}
		if len(segments) == 0 {
			segments = []string{""}
		}

		segRuneStart := 0
		for segIdx, seg := range segments {
			// How many trailing runes of seg are real content vs the
			// truncation tail — search spans map into content-rune space.
			segContentRunes := utf8.RuneCountInString(seg) - utf8.RuneCountInString(truncTail)
			// A grapheme wider than contentWidth survives Hardwrap intact;
			// clamp it to a width-safe placeholder instead of emitting a row
			// that overflows the viewport.
			if w := ansi.StringWidth(seg); w > contentWidth {
				seg = ansi.Truncate(seg, contentWidth, "…")
				segContentRunes = 0
			}
			if segContentRunes < 0 {
				segContentRunes = 0
			}
			gutter := blankGutter
			if segIdx == 0 {
				gutter = gutterStyle.Render(strconv.Itoa(i + 1))
			}
			out.rows = append(out.rows, visualRow{
				lineIdx:         i,
				segIdx:          segIdx,
				text:            gutter + " " + style.Render(seg),
				raw:             seg,
				segStartRune:    segRuneStart,
				segContentRunes: segContentRunes,
			})
			segRuneStart += segContentRunes
		}
	}
	return out
}

// lineContentStyle picks the foreground for a diff line kind — the same
// palette the old renderLine used.
func lineContentStyle(kind git.DiffLineKind) lipgloss.Style {
	switch kind {
	case git.DiffLineAdd:
		return lipgloss.NewStyle().Foreground(common.ColorSuccess())
	case git.DiffLineDelete:
		return lipgloss.NewStyle().Foreground(common.ColorError())
	case git.DiffLineHeader:
		return lipgloss.NewStyle().Foreground(common.ColorInfo()).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(common.ColorForeground())
	}
}
