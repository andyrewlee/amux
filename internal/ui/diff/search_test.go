package diff

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/git"
)

func searchFixture() *Model {
	m := newSizedModel()
	m.focused = true
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineHeader, Content: "@@ -1,3 +1,4 @@ func alpha"},
			{Kind: git.DiffLineContext, Content: " keep"},
			{Kind: git.DiffLineDelete, Content: "-alpha line"},
			{Kind: git.DiffLineAdd, Content: "+beta line"},
			{Kind: git.DiffLineHeader, Content: "@@ -10,3 +11,4 @@ func beta"},
			{Kind: git.DiffLineContext, Content: " ctx"},
			{Kind: git.DiffLineAdd, Content: "+beta again"},
		},
		Hunks: []git.Hunk{{StartLine: 0}, {StartLine: 4}},
	}
	return m
}

func pressKey(m *Model, r rune) {
	m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
}

// pressEnter sends a bare enter — Text must stay empty or the key stringifies
// as a literal character and the enter binding never matches.
func pressEnter(m *Model) {
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// `/` enters query-edit mode; typed text lands in the query and cannot
// trigger viewer commands (typing "q" must not close, "j" must not scroll).
func TestSearchEditOwnsInput(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	if !m.Searching() {
		t.Fatal("/ should enter search mode")
	}
	pressKey(m, 'q') // would close in browse mode
	pressKey(m, 'j') // would scroll in browse mode
	if m.query != "qj" {
		t.Fatalf("query should be qj, got %q", m.query)
	}
	if m.scroll != 0 {
		t.Fatalf("typing must not scroll, scroll=%d", m.scroll)
	}
	if !m.Searching() {
		t.Fatal("q inside the query must not close/exit search")
	}
}

// Enter accepts: literal case-insensitive matching across +/-, context, and
// @@ header lines; selection starts at the first match and scrolls to it.
func TestSearchAcceptSelectsFirstMatch(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	for _, r := range "ALPHA" {
		pressKey(m, r)
	}
	if got := len(m.matches); got != 2 {
		t.Fatalf("ALPHA should match 2 lines (case-insensitive), got %d", got)
	}
	pressEnter(m)
	if m.Searching() {
		t.Fatal("enter should leave edit mode")
	}
	if m.matchIdx != 0 || m.matches[0].lineIdx != 0 {
		t.Fatalf("selection should be the @@ header match, got %+v idx=%d", m.matches[0], m.matchIdx)
	}
}

// n/N cycle through every match with wrap-around on both ends.
func TestSearchCycleWraps(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	for _, r := range "beta" {
		pressKey(m, r)
	}
	pressEnter(m)
	if len(m.matches) != 3 {
		t.Fatalf("beta should match 3 spans (+beta line, @@ header, +beta again), got %d", len(m.matches))
	}
	pressKey(m, 'n')
	if m.matchIdx != 1 || m.searchWrapped {
		t.Fatalf("n should advance to match 2, got idx=%d wrapped=%v", m.matchIdx, m.searchWrapped)
	}
	pressKey(m, 'n')
	pressKey(m, 'n') // wraps 2 → 0
	if m.matchIdx != 0 || !m.searchWrapped {
		t.Fatalf("third n should wrap to 0 with wrapped flag, got idx=%d wrapped=%v", m.matchIdx, m.searchWrapped)
	}
	pressKey(m, 'N') // wraps 0 → 2
	if m.matchIdx != 2 || !m.searchWrapped {
		t.Fatalf("N from 0 should wrap to last, got idx=%d", m.matchIdx)
	}
}

// Without an accepted selection n still navigates hunks.
func TestSearchNoQueryKeepsHunkNav(t *testing.T) {
	m := searchFixture()
	pressKey(m, 'n')
	if m.hunkIdx != 1 {
		t.Fatalf("n without a query should advance to the next hunk, got idx=%d", m.hunkIdx)
	}
}

// esc inside the field keeps the query; esc in browse mode clears it — the
// second esc would close, so assert state instead of consuming it.
func TestSearchEscLayers(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	pressKey(m, 'b')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Searching() {
		t.Fatal("esc should leave edit mode")
	}
	if m.query != "b" {
		t.Fatalf("esc-in-edit must retain the query, got %q", m.query)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.query != "" || len(m.matches) != 0 {
		t.Fatalf("esc-in-browse should clear search, got query=%q matches=%d", m.query, len(m.matches))
	}
}

// No-hit queries report 0 matches and leave n/N inert.
func TestSearchNoMatches(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	for _, r := range "zzz" {
		pressKey(m, r)
	}
	pressEnter(m)
	if m.matchIdx != -1 || len(m.matches) != 0 {
		t.Fatalf("no-hit query should have no selection, got idx=%d n=%d", m.matchIdx, len(m.matches))
	}
	if !strings.Contains(ansi.Strip(m.View()), "0 matches") {
		t.Fatal("footer should report 0 matches")
	}
}

// A wrapped match's visual row is the scroll target — scroll lands on the
// row for the matched source line even when it occupies several rows.
func TestSearchScrollsToWrappedMatch(t *testing.T) {
	m := newSizedModel()
	m.width = 40
	m.height = 6
	m.focused = true
	m.wrap = true
	var lines []git.DiffLine
	for i := 0; i < 12; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
	}
	lines = append(lines, git.DiffLine{Kind: git.DiffLineAdd, Content: "+" + strings.Repeat("x", 90) + "needle"})
	m.diff = &git.DiffResult{Lines: lines}
	pressKey(m, '/')
	for _, r := range "needle" {
		pressKey(m, r)
	}
	pressEnter(m)
	matchRow := m.rows().topFor(len(lines) - 1)
	if m.scroll > matchRow {
		t.Fatalf("scroll %d must be at-or-above match row %d", m.scroll, matchRow)
	}
	if matchRow >= m.scroll+m.contentHeight() {
		t.Fatalf("match row %d must be inside the viewport [%d..%d)", matchRow, m.scroll, m.scroll+m.contentHeight())
	}
}

// The selected match renders inverted: the row containing the span gets a
// reverse-video run while non-selected hits stay plain.
func TestSearchSelectedMatchHighlighted(t *testing.T) {
	m := searchFixture()
	pressKey(m, '/')
	for _, r := range "keep" {
		pressKey(m, r)
	}
	pressEnter(m)
	if m.matchIdx != 0 {
		t.Fatalf("selection idx=%d", m.matchIdx)
	}
	rendered := m.View()
	// lipgloss combines SGR attrs — reverse video appears as the `7;` opener.
	if !strings.Contains(rendered, "\x1b[7;") {
		t.Fatalf("selected match should render a reverse-video span:\n%q", rendered)
	}
	// The query sits mid-line; the stripped line text must survive styling.
	if !strings.Contains(ansi.Strip(rendered), " keep") {
		t.Fatal("highlighted row should still show the source text")
	}
}

// Loading a new diff drops stale match spans.
func TestSearchClearsOnDiffReload(t *testing.T) {
	m := searchFixture()
	m.query = "keep"
	m.recomputeMatches()
	m.matchIdx = 0
	m.Update(diffLoaded{diff: &git.DiffResult{Lines: []git.DiffLine{
		{Kind: git.DiffLineContext, Content: " nothing"},
	}}})
	if m.query != "" || len(m.matches) != 0 || m.matchIdx != -1 {
		t.Fatalf("reload should clear search, got query=%q n=%d idx=%d", m.query, len(m.matches), m.matchIdx)
	}
}

// A short diff (single line) accepts a query without panic.
func TestSearchShortDiffNoPanic(t *testing.T) {
	m := newSizedModel()
	m.focused = true
	m.diff = &git.DiffResult{Lines: []git.DiffLine{
		{Kind: git.DiffLineAdd, Content: "+x"},
	}}
	pressKey(m, '/')
	pressKey(m, 'x')
	pressEnter(m)
	pressKey(m, 'n')
	if m.matchIdx != 0 {
		t.Fatalf("single-match n should wrap to 0, got %d", m.matchIdx)
	}
}
