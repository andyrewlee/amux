package diff

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/git"
)

// visibleRows strips ANSI and splits a View into display rows — the only
// honest way to count what a terminal cell grid will show.
func visibleRows(t *testing.T, out string) []string {
	t.Helper()
	return strings.Split(ansi.Strip(out), "\n")
}

func assertRowsFitWidth(t *testing.T, rows []string, width int) {
	t.Helper()
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > width {
			t.Fatalf("row %d is %d cells wide, exceeds width %d: %q", i, w, width, row)
		}
	}
}

// TestVisualRowsTailReachable is the regression for the pathname-gap-style
// scroll bug: a single wrapped source line produced more display rows than
// the viewport while maxScroll stayed zero, so its tail was unreachable.
func TestVisualRowsTailReachable(t *testing.T) {
	m := newSizedModel()
	m.height = 6
	m.width = 40
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: "start " + strings.Repeat("fill ", 40) + "ENDMARKER"},
		},
	}

	// Enable wrap through the real input path.
	m.focused = true
	m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !m.wrap {
		t.Fatal("w key did not toggle wrap")
	}
	if m.rowsBuilt == 0 {
		t.Fatal("wrap toggle did not build the visual-row layout")
	}
	if total := len(m.rows().rows); total <= m.contentHeight() {
		t.Fatalf("fixture must overflow: %d rows vs capacity %d", total, m.contentHeight())
	}

	if !m.CanConsumeWheel() {
		t.Fatal("wheel must be consumable when visual rows overflow the viewport")
	}

	// End must land within the scrollable range and show the tail marker.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.scroll != m.maxScroll() {
		t.Fatalf("end key scroll = %d, want maxScroll %d", m.scroll, m.maxScroll())
	}
	out := m.View()
	if !strings.Contains(ansi.Strip(out), "ENDMARKER") {
		t.Fatalf("tail marker unreachable after End\n--- visible ---\n%s", ansi.Strip(out))
	}

	// Output is bounded: exactly height display rows, none wider than width.
	rows := visibleRows(t, out)
	if len(rows) != m.height {
		t.Fatalf("View emitted %d display rows, want %d", len(rows), m.height)
	}
	assertRowsFitWidth(t, rows, m.width)

	// Footer (scroll position) is the last emitted row — inside the viewport.
	if !strings.Contains(rows[len(rows)-1], "/") {
		t.Fatalf("footer not visible in last row: %q", rows[len(rows)-1])
	}
}

// TestVisualRowsWheelAndKeysReachTail drives wheel and Down/PgDown input to
// the bottom of a wrapped diff.
func TestVisualRowsWheelAndKeysReachTail(t *testing.T) {
	m := newSizedModel()
	m.height = 6
	m.width = 40
	m.focused = true
	m.wrap = true
	m.diff = &git.DiffResult{
		Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: strings.Repeat("a", 200)},
			{Kind: git.DiffLineAdd, Content: "tail marker " + strings.Repeat("z", 100)},
		},
	}

	for i := 0; i < 3; i++ {
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	out := ansi.Strip(m.View())
	if !strings.Contains(out, strings.Repeat("z", 20)) {
		t.Fatalf("wrapped tail not reached by wheel+keys\n--- visible ---\n%s", out)
	}
	if m.scroll > m.maxScroll() {
		t.Fatalf("scroll %d exceeded max %d", m.scroll, m.maxScroll())
	}
}

// TestVisualRowsBuild covers layout basics: empty lines still emit a row,
// escapes are stripped, continuation rows get blank gutters, and narrow
// widths never overflow.
func TestVisualRowsBuild(t *testing.T) {
	t.Run("empty line emits one row", func(t *testing.T) {
		m := newSizedModel()
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: "first"},
			{Kind: git.DiffLineContext, Content: ""},
			{Kind: git.DiffLineContext, Content: "third"},
		}}
		rows := m.rows()
		if len(rows.rows) != 3 {
			t.Fatalf("got %d rows for 3 lines (one empty), want 3", len(rows.rows))
		}
		if rows.topFor(2) != 2 {
			t.Fatalf("firstRow[2] = %d, want 2", rows.topFor(2))
		}
	})

	t.Run("escapes stripped before styling", func(t *testing.T) {
		m := newSizedModel()
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineAdd, Content: "safe\x1b]8;;https://x\x07link\x1b]8;;\x07"},
		}}
		row := m.rows().rows[0].text
		if strings.Contains(row, "\x1b]") {
			t.Fatalf("row leaked OSC sequence: %q", row)
		}
		if !strings.Contains(ansi.Strip(row), "safelink") {
			t.Fatalf("row lost visible text: %q", row)
		}
	})

	t.Run("continuation rows have blank gutter", func(t *testing.T) {
		m := newSizedModel()
		m.width = 30
		m.wrap = true
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: strings.Repeat("w", 80)},
		}}
		rows := m.rows()
		if len(rows.rows) < 2 {
			t.Fatalf("expected wrapped continuation rows, got %d", len(rows.rows))
		}
		for i, r := range rows.rows {
			visible := ansi.Strip(r.text)
			if i == 0 {
				if !strings.Contains(visible, " 1 ") && !strings.HasPrefix(visible, "  1") {
					t.Fatalf("first segment missing line number gutter: %q", visible)
				}
			} else if strings.TrimLeft(visible[:4], " ") != "" && strings.Contains(visible[:4], "1") {
				t.Fatalf("continuation row %d shows a gutter digit: %q", i, visible)
			}
			if r.segIdx != i {
				t.Fatalf("row %d segIdx = %d", i, r.segIdx)
			}
		}
	})

	t.Run("narrow viewport stays bounded", func(t *testing.T) {
		m := newSizedModel()
		m.width = 8
		m.height = 5
		m.wrap = true
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: strings.Repeat("x", 50)},
		}}
		out := m.View()
		for _, row := range visibleRows(t, out) {
			if w := ansi.StringWidth(row); w > 8 {
				t.Fatalf("row %d cells wide at width 8: %q", w, row)
			}
		}
	})
}

// TestVisualRowsUnicode exercises CJK (2-cell) and combining graphemes
// through wrap and truncate without overflow or invalid UTF-8.
func TestVisualRowsUnicode(t *testing.T) {
	content := strings.Repeat("日本語", 20) + "e\u0301 tail"
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrap=%v", wrap), func(t *testing.T) {
			m := newSizedModel()
			m.width = 24
			m.wrap = wrap
			m.diff = &git.DiffResult{Lines: []git.DiffLine{
				{Kind: git.DiffLineAdd, Content: content},
			}}
			out := m.View()
			if !utf8.ValidString(out) {
				t.Fatal("View produced invalid UTF-8")
			}
			rows := m.rows()
			if !wrap && len(rows.rows) != 1 {
				t.Fatalf("unwrapped long line must be one row, got %d", len(rows.rows))
			}
			if wrap && len(rows.rows) < 3 {
				t.Fatalf("CJK content at width ~19 must wrap to several rows, got %d", len(rows.rows))
			}
			for i, r := range rows.rows {
				if w := ansi.StringWidth(ansi.Strip(r.text)); w > 24 {
					t.Fatalf("row %d overflow: %d cells > 24", i, w)
				}
			}
		})
	}
}

// TestVisualRowsCache proves the wrapped layout survives scrolling and
// repeated View calls without a rebuild — and that the inputs that do shape
// layout invalidate it.
func TestVisualRowsCache(t *testing.T) {
	m := newSizedModel()
	m.width = 30
	m.height = 5
	m.wrap = true
	m.focused = true
	m.diff = &git.DiffResult{Lines: []git.DiffLine{
		{Kind: git.DiffLineContext, Content: strings.Repeat("q", 100)},
		{Kind: git.DiffLineAdd, Content: strings.Repeat("r", 100)},
	}}

	_ = m.rows()
	built := m.rowsBuilt
	if built != 1 {
		t.Fatalf("expected 1 build, got %d", built)
	}

	m.scrollDown(3)
	_ = m.View()
	m.scrollDown(2)
	_ = m.View()
	_ = m.View()
	if m.rowsBuilt != built {
		t.Fatalf("scrolling/View rebuilt layout: builds %d → %d", built, m.rowsBuilt)
	}

	// A width change is a layout input — it must rebuild exactly once.
	m.SetSize(60, 5)
	_ = m.View()
	if m.rowsBuilt != built+1 {
		t.Fatalf("width change should rebuild once, builds = %d", m.rowsBuilt)
	}

	// A height-only change must not rebuild.
	m.SetSize(60, 9)
	_ = m.View()
	if m.rowsBuilt != built+1 {
		t.Fatalf("height-only resize rebuilt layout: builds = %d", m.rowsBuilt)
	}
}

// TestVisualRowsAnchorOnWrapToggle: toggling wrap keeps the same source line
// at the viewport top.
func TestVisualRowsAnchorOnWrapToggle(t *testing.T) {
	m := newSizedModel()
	m.width = 30
	m.height = 5
	m.focused = true
	var lines []git.DiffLine
	for i := 0; i < 10; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("line-%d %s", i, strings.Repeat("x", 60))})
	}
	m.diff = &git.DiffResult{Lines: lines}

	m.scroll = 6 // unwrapped: source line 6 at the top
	m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !m.wrap {
		t.Fatal("wrap not enabled")
	}
	top := m.rows().rows[m.scroll]
	if top.lineIdx != 6 {
		t.Fatalf("after wrap, top row belongs to line %d, want 6", top.lineIdx)
	}
}

// TestVisualRowsHunkCycleNearBottom: the last hunk can sit below the
// scrollable range — n must still cycle through it rather than sticking.
func TestVisualRowsHunkCycleNearBottom(t *testing.T) {
	m := newSizedModel()
	m.width = 40
	m.height = 6
	m.focused = true
	m.wrap = true
	var lines []git.DiffLine
	for i := 0; i < 8; i++ {
		lines = append(lines, git.DiffLine{Kind: git.DiffLineContext, Content: fmt.Sprintf("ctx-%d", i)})
	}
	// Hunk 1 near the start; hunk 2's source line wraps so far its top row is
	// beyond maxScroll — its own content is reachable only by clamping.
	lines = append(lines, git.DiffLine{Kind: git.DiffLineAdd, Content: strings.Repeat("y", 120)})
	m.diff = &git.DiffResult{
		Lines: lines,
		Hunks: []git.Hunk{{StartLine: 2}, {StartLine: 8}},
	}

	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.hunkIdx != 0 || m.scroll == 0 {
		t.Fatalf("first n should select hunk 1, got idx=%d scroll=%d", m.hunkIdx, m.scroll)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.hunkIdx != 1 {
		t.Fatalf("second n should select hunk 2, got idx=%d", m.hunkIdx)
	}
	if m.scroll > m.maxScroll() {
		t.Fatalf("hunk 2 scroll %d exceeds max %d", m.scroll, m.maxScroll())
	}
	// Cycling returns to the first hunk.
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.hunkIdx != 0 {
		t.Fatalf("third n should wrap to hunk 1, got idx=%d", m.hunkIdx)
	}
	// p from the top cycles back to the clamped last hunk.
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.hunkIdx != 1 {
		t.Fatalf("p should wrap to last hunk, got idx=%d", m.hunkIdx)
	}
}

// TestVisualRowsChromeTiers pins the viewport tiers: h≤0 empty, h=1 header
// only, h=2 header+footer, h≥3 header+stats+content+footer.
func TestVisualRowsChromeTiers(t *testing.T) {
	build := func(w, h int) *Model {
		m := newSizedModel()
		m.width = w
		m.height = h
		m.diff = &git.DiffResult{Lines: []git.DiffLine{
			{Kind: git.DiffLineContext, Content: "body"},
		}}
		return m
	}

	if out := build(80, 0).View(); out != "" {
		t.Fatalf("height 0 must render empty, got %q", out)
	}
	if out := build(0, 5).View(); out != "" {
		t.Fatalf("width 0 must render empty, got %q", out)
	}
	if rows := visibleRows(t, build(80, 1).View()); len(rows) != 1 || !strings.Contains(rows[0], "foo.go") {
		t.Fatalf("height 1 must show only the header, got %v", rows)
	}
	if rows := visibleRows(t, build(80, 2).View()); len(rows) != 2 || !strings.Contains(rows[1], "wrap") {
		t.Fatalf("height 2 must show header+footer, got %v", rows)
	}
	rows := visibleRows(t, build(80, 5).View())
	if len(rows) != 5 {
		t.Fatalf("height 5 must emit exactly 5 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[1], "+0") || !strings.Contains(rows[2], "body") || !strings.Contains(rows[4], "wrap") {
		t.Fatalf("height 5 tiers wrong: %v", rows)
	}
}
