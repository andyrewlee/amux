package diff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

// selectedHitRow returns the first visual row of the selected match's source
// line whose segment intersects the selected rune span — the row scrollToMatch
// is required to bring into view. It is an independent re-derivation over
// public row metadata, so it cannot silently agree with a broken helper.
func selectedHitRow(t *testing.T, m *Model) int {
	t.Helper()
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) {
		t.Fatalf("no selected match (idx=%d, n=%d)", m.matchIdx, len(m.matches))
	}
	sel := m.matches[m.matchIdx]
	rows := m.rows()
	for i := rows.topFor(sel.lineIdx); i < len(rows.rows) && rows.rows[i].lineIdx == sel.lineIdx; i++ {
		if lo, _ := rowHighlight(rows.rows[i], sel); lo >= 0 {
			return i
		}
	}
	t.Fatalf("selected span %+v intersects no displayed segment", sel)
	return -1
}

func requireRowVisible(t *testing.T, m *Model, row int) {
	t.Helper()
	if row < m.scroll || row >= m.scroll+m.contentHeight() {
		t.Fatalf("hit row %d outside viewport [%d..%d)", row, m.scroll, m.scroll+m.contentHeight())
	}
}

// typeQuery enters /, types the query, and accepts it.
func typeQuery(m *Model, query string) {
	pressKey(m, '/')
	for _, r := range query {
		pressKey(m, r)
	}
	pressEnter(m)
}

// TestSearchWrappedMatchMultipleOccurrences: accept, n, and N must each bring
// the SELECTED occurrence's segment into view — a different hit of the same
// query happening to be onscreen does not satisfy the contract.
func TestSearchWrappedMatchMultipleOccurrences(t *testing.T) {
	m := newSizedModel()
	m.width = 40 // contentWidth 36
	m.height = 6 // contentHeight 3
	m.focused = true
	m.wrap = true
	var lines []git.DiffLine
	for i := 0; i < 12; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
	}
	// One line, two "hit" spans in different wrapped segments: the first in
	// seg0 (runes 1-4), the second in seg2 (runes 94-97 of 97).
	lines = append(lines, git.DiffLine{
		Kind:    git.DiffLineAdd,
		Content: "+" + "hit" + strings.Repeat("x", 90) + "hit",
	})
	m.diff = &git.DiffResult{Lines: lines}

	typeQuery(m, "hit")
	if len(m.matches) != 2 {
		t.Fatalf("expected two occurrences on the line, got %v", m.matches)
	}
	first, second := m.matches[0], m.matches[1]
	rows := m.rows()
	if rows.topFor(first.lineIdx) != rows.topFor(second.lineIdx) {
		t.Fatal("fixture needs both hits on one source line")
	}
	requireRowVisible(t, m, selectedHitRow(t, m))

	pressKey(m, 'n')
	if m.matchIdx != 1 {
		t.Fatalf("n should select occurrence 2, got %d", m.matchIdx)
	}
	hitRow := selectedHitRow(t, m)
	requireRowVisible(t, m, hitRow)
	// The selected span's segment — not merely the same line's first
	// segment — must be visible and carry the reverse-video span.
	rendered := m.renderRowMatch(rows.rows[hitRow], second)
	if !strings.Contains(rendered, "\x1b[7;") {
		t.Fatal("selected occurrence must render highlighted")
	}

	pressKey(m, 'N')
	if m.matchIdx != 0 {
		t.Fatalf("N should wrap back to occurrence 1, got %d", m.matchIdx)
	}
	requireRowVisible(t, m, selectedHitRow(t, m))
}

// TestSearchWrappedMatchBoundary: a hit spanning a wrap boundary resolves to
// its FIRST intersecting segment, which must be visible and highlighted.
func TestSearchWrappedMatchBoundary(t *testing.T) {
	m := newSizedModel()
	m.width = 40 // contentWidth 36
	m.height = 6
	m.focused = true
	m.wrap = true
	var lines []git.DiffLine
	for i := 0; i < 10; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
	}
	// "needle" starts at rune 35 and crosses the seg0/seg1 boundary at 36.
	lines = append(lines, git.DiffLine{
		Kind:    git.DiffLineAdd,
		Content: "+" + strings.Repeat("x", 34) + "needle",
	})
	m.diff = &git.DiffResult{Lines: lines}

	typeQuery(m, "needle")
	if len(m.matches) != 1 {
		t.Fatalf("expected one match, got %v", m.matches)
	}
	sel := m.matches[0]
	if sel.startRune != 35 || sel.endRune != 41 {
		t.Fatalf("selected span = [%d,%d), want [35,41) crossing the boundary", sel.startRune, sel.endRune)
	}
	rows := m.rows()
	hitRow := selectedHitRow(t, m)
	if hitRow != rows.topFor(sel.lineIdx) {
		t.Fatalf("first intersection should be seg0 (row %d), got %d", rows.topFor(sel.lineIdx), hitRow)
	}
	requireRowVisible(t, m, hitRow)
	rendered := m.renderRowMatch(rows.rows[hitRow], sel)
	if !strings.Contains(rendered, "\x1b[7;") {
		t.Fatal("boundary intersection must render highlighted")
	}
	// Sanity: the same span also intersects the next segment.
	if lo, _ := rowHighlight(rows.rows[hitRow+1], sel); lo < 0 {
		t.Fatal("fixture must place the span across the seg0/seg1 boundary")
	}
}

// TestSearchWrappedMatchHunkContext: the enclosing @@ header stays anchored
// only when it fits with the actual matching segment — when it fits with the
// line's first segment but not the hit's, the hit wins.
func TestSearchWrappedMatchHunkContext(t *testing.T) {
	cases := []struct {
		name          string
		lines         []git.DiffLine
		wantScrollTop bool // anchored at the header row
	}{
		{
			name: "header fits with the hit segment",
			lines: []git.DiffLine{
				{Kind: git.DiffLineHeader, Content: "@@ -1 +1,3 @@ func f"},
				// Hit in seg1 (runes 37-43): intersecting row 2, within
				// 3 rows of the header — the header stays anchored.
				{Kind: git.DiffLineAdd, Content: "+" + strings.Repeat("x", 36) + "needle"},
			},
			wantScrollTop: true,
		},
		{
			name: "hit outranks a header that only fits the first segment",
			lines: []git.DiffLine{
				{Kind: git.DiffLineHeader, Content: "@@ -1 +1,3 @@ func f"},
				// First segment is row 1 (fits with header at capacity 3),
				// but the hit lives in seg2 (row 3) — anchoring the header
				// would show rows 0-2 and hide the hit.
				{Kind: git.DiffLineAdd, Content: "+" + strings.Repeat("x", 90) + "needle"},
			},
			wantScrollTop: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newSizedModel()
			m.width = 40
			m.height = 6
			m.focused = true
			m.wrap = true
			m.diff = &git.DiffResult{
				Lines: tc.lines,
				Hunks: []git.Hunk{{StartLine: 0}},
			}
			typeQuery(m, "needle")
			requireRowVisible(t, m, selectedHitRow(t, m))
			if tc.wantScrollTop && m.scroll != 0 {
				t.Fatalf("header should stay anchored at top, scroll=%d", m.scroll)
			}
			if !tc.wantScrollTop && m.scroll == 0 {
				t.Fatal("scroll must not anchor the header when that hides the hit")
			}
		})
	}
}

// TestSearchWrappedMatchLayoutChanges: after a geometry change (resize,
// wrap toggle) navigation must resolve the selection against the CURRENT
// row metadata — never a cached coordinate from the old layout.
func TestSearchWrappedMatchLayoutChanges(t *testing.T) {
	m := newSizedModel()
	m.width = 40
	m.height = 6
	m.focused = true
	m.wrap = true
	var lines []git.DiffLine
	for i := 0; i < 12; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
	}
	lines = append(lines, git.DiffLine{
		Kind:    git.DiffLineAdd,
		Content: "+" + strings.Repeat("x", 90) + "needle",
	})
	m.diff = &git.DiffResult{Lines: lines}
	typeQuery(m, "needle")
	requireRowVisible(t, m, selectedHitRow(t, m))

	// Resize wider: contentWidth 76 re-wraps the line into 2 segments with
	// the hit in seg1 — navigation must find it under the new layout.
	m.SetSize(80, 6)
	pressKey(m, 'n') // wraps the single match back to itself, re-scrolling
	requireRowVisible(t, m, selectedHitRow(t, m))

	// Toggle wrap off: the needle falls into the truncated tail, so no
	// segment intersects — navigation falls back to the line's first row.
	pressKey(m, 'w')
	pressKey(m, 'n')
	sel := m.matches[m.matchIdx]
	want := m.rows().topFor(sel.lineIdx)
	requireRowVisible(t, m, want)

	// Toggle wrap back on: the hit segment is displayed again and the
	// intersecting-row contract resumes.
	pressKey(m, 'w')
	pressKey(m, 'n')
	requireRowVisible(t, m, selectedHitRow(t, m))
}

// TestSearchWrappedMatchFallback: when no displayed segment can show the hit
// (truncated tail, placeholder row) or there is no content capacity or
// selection, scrolling must stay clamped and panic-free on the source-row
// fallback rather than chasing an undisplayable span.
func TestSearchWrappedMatchFallback(t *testing.T) {
	longTail := func() []git.DiffLine {
		var lines []git.DiffLine
		for i := 0; i < 12; i++ {
			lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
		}
		return append(lines, git.DiffLine{
			Kind:    git.DiffLineAdd,
			Content: "+" + strings.Repeat("x", 90) + "needle",
		})
	}

	t.Run("nowrap hidden tail falls back to the line's row", func(t *testing.T) {
		m := newSizedModel()
		m.width = 40
		m.height = 6
		m.focused = true
		m.wrap = false // needle truncates into the "..." tail
		m.diff = &git.DiffResult{Lines: longTail()}
		typeQuery(m, "needle")
		sel := m.matches[m.matchIdx]
		row := m.rows().topFor(sel.lineIdx)
		requireRowVisible(t, m, row)
		if m.scroll < 0 || m.scroll > m.maxScroll() {
			t.Fatalf("scroll %d outside [0..%d]", m.scroll, m.maxScroll())
		}
	})

	t.Run("width-safe placeholder keeps the fallback", func(t *testing.T) {
		m := newSizedModel()
		m.width = 6 // contentWidth 2: a wide grapheme overflows into "…"
		m.height = 6
		m.focused = true
		m.wrap = true
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineAdd, Content: "🙂needle"},
		}}
		typeQuery(m, "needle")
		if m.matchIdx < 0 {
			t.Fatal("expected a selected match")
		}
		row := m.rows().topFor(m.matches[m.matchIdx].lineIdx)
		requireRowVisible(t, m, row)
	})

	t.Run("invalid selection is inert", func(t *testing.T) {
		m := newSizedModel()
		m.diff = &git.DiffResult{Lines: longTail()}
		m.scroll = 2
		m.matchIdx = -1
		m.scrollToMatch()
		if m.scroll != 2 {
			t.Fatalf("no selection must not scroll, got %d", m.scroll)
		}
		m.matches = []diffMatch{{lineIdx: 0}}
		m.matchIdx = 5 // out of bounds
		m.scrollToMatch()
		if m.scroll != 2 {
			t.Fatalf("out-of-range selection must not scroll, got %d", m.scroll)
		}
	})

	t.Run("height <=3 has no capacity but stays safe", func(t *testing.T) {
		m := newSizedModel()
		m.width = 40
		m.height = 3 // header+stats+footer consume everything
		m.focused = true
		m.wrap = true
		m.diff = &git.DiffResult{Lines: longTail()}
		typeQuery(m, "needle")
		if m.contentHeight() != 0 {
			t.Fatalf("height 3 must have zero content capacity, got %d", m.contentHeight())
		}
		if m.scroll != 0 {
			t.Fatalf("zero-capacity scroll must clamp to 0, got %d", m.scroll)
		}
	})
}
