package vterm

import (
	"strings"
	"testing"
)

// Characterization tests for four specific vterm edge cases with concrete
// visual-corruption consequences. These pin CURRENT behavior so refactors
// (notably the epoch-based render cache) cannot silently regress them. Where a
// test documents a known limitation rather than correct behavior, that is
// called out inline. See plans/012-vterm-characterization-tests.md.

// TestCombiningCharacterIsDropped pins the combining-mark drop in
// ops.go putChar's width==0 branch.
//
// characterizes current behavior — combining marks are intentionally dropped
// (ops.go width==0 no-op); update this test if real combining support lands.
//
// Writing precomposed "é" (U+00E9, width 1) works, but a base letter followed
// by a combining acute (U+0301, width 0) drops the accent: the branch finds the
// previous cell, does `_ = cell // no-op`, and returns WITHOUT advancing the
// cursor. So "éX" renders as bare "eX" with the cursor at column 2 (the
// combining rune consumed no column). This is the explicit record that NFD text
// and complex emoji are not yet rendered — a known limitation.
func TestCombiningCharacterIsDropped(t *testing.T) {
	t.Parallel()

	v := New(10, 3)
	// 'e' + U+0301 COMBINING ACUTE ACCENT + 'X'.
	v.Write([]byte("éX"))

	row := v.Screen[0]
	// The combining mark left no cell of its own: column 0 is 'e', column 1 is
	// 'X', column 2 onward is blank. The accent (U+0301) appears nowhere.
	if row[0].Rune != 'e' {
		t.Fatalf("column 0: got %q want 'e'", row[0].Rune)
	}
	if row[1].Rune != 'X' {
		t.Fatalf("column 1: got %q want 'X' (combining mark should have been dropped, not stored)", row[1].Rune)
	}
	// The cell that received 'e' must not have been mutated to carry the accent.
	if row[0].Rune != 'e' || row[0].Width != 1 {
		t.Fatalf("column 0 cell mutated by combining mark: %+v", row[0])
	}
	// The combining rune must not appear anywhere on the line.
	for x, c := range row {
		if c.Rune == '́' {
			t.Fatalf("combining mark U+0301 unexpectedly present at column %d", x)
		}
	}
	// Cursor advanced by 'e' (1) + 'X' (1) only; the combining mark advanced
	// nothing because putChar returns early without bumping CursorX.
	if v.CursorX != 2 {
		t.Fatalf("CursorX: got %d want 2 (combining mark must not advance the cursor)", v.CursorX)
	}
	if v.CursorY != 0 {
		t.Fatalf("CursorY: got %d want 0", v.CursorY)
	}
}

// TestCursorOnlyMoveMarksCursorLinesDirty pins the cursor-only-move dirty
// contract at the vterm boundary.
//
// Pins contract (b) from the plan: a BARE cursor move (no cell content change)
// does NOT show up in the DirtyLines() fast path — bumpVersionIfCursorMoved
// (accessors.go) bumps only `version`, never a render epoch, so DirtyLines()
// (cache.go) reports zero dirty lines. The compensation lives entirely in the
// full Render() path (renderScreenCached, render.go), which invalidates the old
// and new cursor lines. This is the documented DirtyLines gap: production is
// safe only because the compositor re-derives invalidation from
// LastCursorState(); a "trust DirtyLines()" simplification would resurrect a
// stuck cursor block. This test pins that SOMETHING (Render, not DirtyLines)
// invalidates.
func TestCursorOnlyMoveMarksCursorLinesDirty(t *testing.T) {
	t.Parallel()

	v := New(10, 5)
	v.ShowCursor = true

	// Park the cursor on the last row and warm the cache.
	v.Write([]byte("\x1b[5;1H"))
	first := v.Render()
	v.ClearDirty()

	// Confirm the cursor block (reverse video) starts on the last row.
	firstLines := strings.Split(first, "\n")
	if !lineHasReverse(firstLines[4]) {
		t.Fatalf("expected cursor (reverse video) on row 4 in first render:\n%s",
			strings.ReplaceAll(first, "\x1b", "<ESC>"))
	}

	// Bare cursor move to the top row: no cell content changes.
	v.Write([]byte("\x1b[1;1H"))

	// Contract (b): the DirtyLines() fast path reports NOTHING dirty after a
	// bare cursor move. Pin that documented gap.
	dirty, all := v.DirtyLines()
	if all {
		t.Fatalf("DirtyLines() reported all-dirty; expected the documented fast-path gap (none dirty)")
	}
	for y, d := range dirty {
		if d {
			t.Fatalf("DirtyLines() reported line %d dirty after a bare cursor move; "+
				"expected the documented fast-path gap (none dirty)", y)
		}
	}

	// But full Render() compensates: the cursor block must now be on the new row
	// (row 0) and gone from the old row (row 4).
	second := v.Render()
	secondLines := strings.Split(second, "\n")
	if !lineHasReverse(secondLines[0]) {
		t.Fatalf("Render() did not move the cursor block to the new row 0:\n%s",
			strings.ReplaceAll(second, "\x1b", "<ESC>"))
	}
	if lineHasReverse(secondLines[4]) {
		t.Fatalf("Render() left a stale cursor block on the old row 4:\n%s",
			strings.ReplaceAll(second, "\x1b", "<ESC>"))
	}
}

// lineHasReverse reports whether a rendered line contains a reverse-video SGR
// introduction, which is how renderRow draws the cursor cell.
func lineHasReverse(line string) bool {
	// The cursor cell is emitted with reverse on. renderRow uses "7" for reverse
	// either as a standalone SGR (\x1b[7m) or combined (\x1b[0;7m / ...;7m).
	for _, seq := range []string{"\x1b[7m", ";7m", "[7m"} {
		if strings.Contains(line, seq) {
			return true
		}
	}
	return false
}

// TestResizePreservesSGRAndPartialEscape pins that a resize mid-output preserves
// both the active SGR style and an in-flight (partial) escape sequence.
//
// Pins CORRECT behavior: vterm.go resize never touches CurrentStyle or the
// parser state, so a color set before a resize survives and a CSI split across a
// resize still parses. A "clean slate on resize" change would break color
// continuity for apps that resize mid-frame.
func TestResizePreservesSGRAndPartialEscape(t *testing.T) {
	t.Parallel()

	// (a) SGR set before resize survives the resize.
	v := New(10, 4)
	v.Write([]byte("\x1b[31m")) // red foreground
	v.Resize(20, 6)
	v.Write([]byte("R"))

	// Find the cell holding 'R'.
	cell, ok := findRune(v.Screen, 'R')
	if !ok {
		t.Fatalf("did not find 'R' on screen after resize")
	}
	if cell.Style.Fg.Type != ColorIndexed || cell.Style.Fg.Value != 1 {
		t.Fatalf("foreground not preserved across resize: got type=%v value=%d want indexed red (1)",
			cell.Style.Fg.Type, cell.Style.Fg.Value)
	}

	// (b) A partial CSI split across a resize still completes.
	v2 := New(10, 4)
	v2.Write([]byte("\x1b[3")) // partial CSI: "ESC [ 3"
	v2.Resize(20, 6)
	v2.Write([]byte("1mZ")) // completes "ESC [ 31 m" then prints 'Z'

	zCell, ok := findRune(v2.Screen, 'Z')
	if !ok {
		t.Fatalf("did not find 'Z' on screen; partial escape likely printed literally after resize")
	}
	// The escape completed: 'Z' is styled red, and none of the escape bytes
	// ('3', '1', 'm') were printed literally.
	if zCell.Style.Fg.Type != ColorIndexed || zCell.Style.Fg.Value != 1 {
		t.Fatalf("partial escape did not complete across resize: 'Z' fg got type=%v value=%d want indexed red (1)",
			zCell.Style.Fg.Type, zCell.Style.Fg.Value)
	}
	if _, found := findRune(v2.Screen, 'm'); found {
		t.Fatalf("escape byte 'm' printed literally; partial escape state was lost on resize")
	}
}

// findRune returns the first cell on screen holding r, scanning row-major.
func findRune(screen [][]Cell, r rune) (Cell, bool) {
	for _, row := range screen {
		for _, c := range row {
			if c.Rune == r {
				return c, true
			}
		}
	}
	return Cell{}, false
}

// TestRenderAtMaxScrollbackBoundary pins rendering with the viewport scrolled
// fully into a maxed-out scrollback history.
//
// Pins CORRECT behavior: feeding more than MaxScrollback lines trims scrollback
// to exactly MaxScrollback (vterm.go trimScrollback), and scrolling the viewport
// to the maximum offset (vterm_scroll.go ScrollViewToTop) renders the oldest
// RETAINED line at the top row — not blank, not a trimmed-away line, no panic.
// This is the off-by-one-prone extreme previously only tested for "didn't crash".
func TestRenderAtMaxScrollbackBoundary(t *testing.T) {
	t.Parallel()

	v := New(20, 5)
	// Feed well over MaxScrollback newline-terminated lines so the screen scrolls
	// content into scrollback. Each line is uniquely numbered so we can identify
	// which lines survived the trim.
	total := MaxScrollback + 200
	var b strings.Builder
	for i := 0; i < total; i++ {
		b.WriteString("L")
		b.WriteString(itoa(i))
		b.WriteString("\r\n")
	}
	v.Write([]byte(b.String()))

	// Scrollback must be trimmed to exactly the cap.
	if len(v.Scrollback) != MaxScrollback {
		t.Fatalf("scrollback length: got %d want %d (MaxScrollback)", len(v.Scrollback), MaxScrollback)
	}

	// Scroll the viewport fully to the top of retained history.
	v.ScrollViewToTop()
	offset, maxOffset := v.GetScrollInfo()
	if offset != maxOffset {
		t.Fatalf("ScrollViewToTop did not reach max offset: offset=%d max=%d", offset, maxOffset)
	}
	if offset != len(v.Scrollback) {
		t.Fatalf("max view offset: got %d want %d (len(Scrollback))", offset, len(v.Scrollback))
	}

	// Rendering at the cap must not panic and must show the oldest RETAINED
	// scrollback line at the top row. The oldest retained line is Scrollback[0].
	out := v.Render()
	topRow := strings.SplitN(out, "\n", 2)[0]
	want := plainLineText(v.Scrollback[0])
	if want == "" {
		t.Fatalf("oldest retained scrollback line is blank; expected real content")
	}
	if !strings.Contains(stripANSI(topRow), want) {
		t.Fatalf("top rendered row does not show oldest retained scrollback line %q:\ntop row: %q",
			want, stripANSI(topRow))
	}

	// The oldest line that was trimmed away (L0) must NOT be the top line.
	if strings.Contains(stripANSI(topRow), "L0 ") || stripANSI(strings.TrimRight(topRow, " ")) == "L0" {
		t.Fatalf("top row shows a trimmed-away line: %q", stripANSI(topRow))
	}
}

// itoa is a tiny dependency-free integer formatter for building test input.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// plainLineText returns the line's runes trimmed of trailing blanks.
func plainLineText(cells []Cell) string {
	var b strings.Builder
	for _, c := range cells {
		if c.Rune != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// stripANSI removes ANSI escape sequences from a rendered string so content can
// be asserted on directly.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			// Skip "ESC [ ... <final byte 0x40-0x7e>".
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++ // consume the final byte
				}
				i = j
				continue
			}
			// Lone ESC; skip it.
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
