package vterm

import (
	"fmt"
	"testing"
)

func TestAltScreenEraseNoOverlapWhenContentDiffers(t *testing.T) {
	// Verify that overlap detection does not falsely match when content changes.
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Frame 1: overflow with AAA, BBB, CCC, DDD (AAA scrolls off)
	vt.Write([]byte("AAA\r\nBBB\r\nCCC\r\nDDD"))
	vt.Write([]byte("\x1b[2J"))

	// Frame 2: completely different content that overflows
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("XXX\r\nYYY\r\nZZZ\r\nWWW"))
	vt.Write([]byte("\x1b[2J"))

	// All new content should be present — no false dedup
	found := map[string]bool{}
	for _, line := range vt.Scrollback {
		text := lineText(line)
		if text != "" {
			found[text] = true
		}
	}

	for _, want := range []string{"YYY", "ZZZ", "WWW"} {
		if !found[want] {
			t.Errorf("expected %q in scrollback, not found", want)
			dumpScrollback(t, vt)
			break
		}
	}
}

func TestAltScreenErasePartialOverlap(t *testing.T) {
	// Manually set up scrollback to have specific tail lines,
	// then verify partial overlap detection works.
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// makeLine creates a []Cell row with given text.
	makeLine := func(text string) []Cell {
		line := MakeBlankLine(10)
		for i, r := range text {
			if i >= 10 {
				break
			}
			line[i] = Cell{Rune: r, Width: 1}
		}
		return line
	}

	// Pre-populate scrollback with lines that partially overlap
	// with what the screen capture will produce.
	vt.Scrollback = append(vt.Scrollback, makeLine("alpha"))
	vt.Scrollback = append(vt.Scrollback, makeLine("beta"))

	// Put "beta" and "gamma" on screen (beta overlaps with scrollback tail)
	vt.Screen[0] = makeLine("beta")
	vt.Screen[1] = makeLine("gamma")
	// row 2 is blank

	vt.Write([]byte("\x1b[2J"))

	// Should have: alpha, beta, gamma (not alpha, beta, beta, gamma)
	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected 3 scrollback lines, got %d", len(vt.Scrollback))
	}

	expected := []string{"alpha", "beta", "gamma"}
	for i, want := range expected {
		got := lineText(vt.Scrollback[i])
		if got != want {
			t.Errorf("scrollback[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestAltScreenEraseOverlapAcrossMultipleRedraws(t *testing.T) {
	// Verify duplication doesn't compound over 5 erase cycles
	// with content that overflows the terminal.
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// First draw
	vt.Write([]byte("line1\r\nline2\r\nline3\r\nline4\r\nline5"))
	vt.Write([]byte("\x1b[2J"))
	firstLen := len(vt.Scrollback)

	for cycle := 1; cycle < 5; cycle++ {
		vt.Write([]byte("\x1b[H"))
		vt.Write([]byte("line1\r\nline2\r\nline3\r\nline4\r\nline5"))
		vt.Write([]byte("\x1b[2J"))
	}

	if len(vt.Scrollback) != firstLen {
		t.Errorf("scrollback should stay stable: first=%d, after5=%d",
			firstLen, len(vt.Scrollback))
		dumpScrollback(t, vt)
	}

	// Verify no adjacent duplicates
	for i := 1; i < len(vt.Scrollback); i++ {
		prev := lineText(vt.Scrollback[i-1])
		cur := lineText(vt.Scrollback[i])
		if prev == cur && prev != "" {
			t.Errorf("adjacent duplicate at index %d: %q", i, cur)
			dumpScrollback(t, vt)
			break
		}
	}
}

func TestAltScreenEraseContentChangeAfterOverflow(t *testing.T) {
	// After overflowing content, changing the content should replace
	// the old capture properly.
	vt := New(10, 4)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Frame 1: 6 lines overflow 4-row terminal
	vt.Write([]byte("aaa\r\nbbb\r\nccc\r\nddd\r\neee\r\nfff"))
	vt.Write([]byte("\x1b[2J"))

	// Frame 2: different 6 lines
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("111\r\n222\r\n333\r\n444\r\n555\r\n666"))
	vt.Write([]byte("\x1b[2J"))

	// Old capture (ccc-fff) should be replaced, not orphaned
	found := map[string]int{}
	for _, line := range vt.Scrollback {
		text := lineText(line)
		if text != "" {
			found[text]++
		}
	}

	// New content should be present
	for _, want := range []string{"111", "222", "333", "444", "555", "666"} {
		if found[want] == 0 {
			t.Errorf("expected %q in scrollback, not found", want)
			dumpScrollback(t, vt)
			return
		}
	}

	// Old scrollUp lines (aaa, bbb) may persist, old capture lines should not
	for _, old := range []string{"ccc", "ddd", "eee", "fff"} {
		if found[old] > 0 {
			t.Errorf("old capture line %q should be removed, found %d times", old, found[old])
			dumpScrollback(t, vt)
			return
		}
	}
}

func TestAltScreenScrollbackTailOverlap(t *testing.T) {
	makeLine := func(text string, width int) []Cell {
		line := MakeBlankLine(width)
		for i, r := range text {
			if i >= width {
				break
			}
			line[i] = Cell{Rune: r, Width: 1}
		}
		return line
	}

	tests := []struct {
		name     string
		sb       []string
		lines    []string
		expected int
	}{
		{"no overlap", []string{"A", "B"}, []string{"C", "D"}, 0},
		{"full overlap", []string{"A", "B"}, []string{"A", "B"}, 2},
		{"partial overlap 1", []string{"A", "B", "C"}, []string{"C", "D"}, 1},
		{"partial overlap 2", []string{"A", "B", "C"}, []string{"B", "C", "D"}, 2},
		{"empty scrollback", nil, []string{"A"}, 0},
		{"empty lines", []string{"A"}, nil, 0},
		{"single match", []string{"X"}, []string{"X", "Y"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb [][]Cell
			for _, s := range tt.sb {
				sb = append(sb, makeLine(s, 5))
			}
			var lines [][]Cell
			for _, s := range tt.lines {
				lines = append(lines, makeLine(s, 5))
			}
			got := scrollbackTailOverlap(sb, lines)
			if got != tt.expected {
				t.Errorf("scrollbackTailOverlap() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestAltScreenEraseManyOverflowCyclesStable(t *testing.T) {
	// Stress test: 20 redraw cycles of overflowing content.
	// Scrollback should stabilize and never grow unbounded.
	vt := New(10, 4)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Generate 8 lines of content (overflows 4-row terminal)
	content := "L01\r\nL02\r\nL03\r\nL04\r\nL05\r\nL06\r\nL07\r\nL08"

	vt.Write([]byte(content))
	vt.Write([]byte("\x1b[2J"))
	stableLen := len(vt.Scrollback)

	for i := 0; i < 20; i++ {
		vt.Write([]byte("\x1b[H"))
		vt.Write([]byte(content))
		vt.Write([]byte("\x1b[2J"))
	}

	if len(vt.Scrollback) != stableLen {
		t.Errorf("scrollback grew after 20 cycles: expected %d, got %d",
			stableLen, len(vt.Scrollback))
		dumpScrollback(t, vt)
	}

	// Verify content is correct: L01..L08, each once
	for i, line := range vt.Scrollback {
		expected := fmt.Sprintf("L%02d", i+1)
		got := lineText(line)
		if got != expected {
			t.Errorf("scrollback[%d] = %q, want %q", i, got, expected)
		}
	}
}

func TestAltScreenErasePartialOverlapReservesFullFrameOnResizeGrow(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	makeLine := func(text string) []Cell {
		line := MakeBlankLine(10)
		for i, r := range text {
			if i >= 10 {
				break
			}
			line[i] = Cell{Rune: r, Width: 1}
		}
		return line
	}

	vt.Scrollback = append(vt.Scrollback, makeLine("alpha"))
	vt.Scrollback = append(vt.Scrollback, makeLine("beta"))
	vt.Screen[0] = makeLine("beta")
	vt.Screen[1] = makeLine("gamma")

	vt.Write([]byte("\x1b[2J"))

	if vt.altScreenCaptureLen != 2 {
		t.Fatalf("captureLen = %d, want 2", vt.altScreenCaptureLen)
	}
	if vt.altScreenCaptureDropLen != 1 {
		t.Fatalf("dropLen = %d, want 1", vt.altScreenCaptureDropLen)
	}
	if !vt.altScreenCaptureTracked {
		t.Fatal("expected partial-overlap capture to stay tracked")
	}

	vt.Resize(10, 4)

	if got := lineText(vt.Screen[0]); got != "alpha" {
		t.Fatalf("expected resize grow to restore only pre-frame history, got %q", got)
	}
	if got := lineText(vt.Screen[1]); got != "" {
		t.Fatalf("expected overlapping frame rows to remain reserved, got %q", got)
	}
}

func TestAltScreenEndOffsetPreservedOnPartialDedup(t *testing.T) {
	// Regression: dedupScrollUpTrailing must not zero altScreenCaptureEndOffset
	// when trailing lines don't overlap with pre-capture content.
	// Scenario: cycle 1 captures [C,D,E], cycle 2 adds scrollUp lines [X,Y]
	// that don't match pre-capture [A,B]. endOffset must remain 2 so cycle 3
	// computes the correct capture position.
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Cycle 1: draw A,B,C,D,E on 3-row screen → A,B scroll off, C/D/E on screen
	vt.Write([]byte("AAA\r\nBBB\r\nCCC\r\nDDD\r\nEEE"))
	vt.Write([]byte("\x1b[2J"))

	// Cycle 2: different above-fold content, same below-fold
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("XXX\r\nYYY\r\nCCC\r\nDDD\r\nEEE"))
	vt.Write([]byte("\x1b[2J"))

	// Cycle 3: identical redraw — should dedup cleanly
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("XXX\r\nYYY\r\nCCC\r\nDDD\r\nEEE"))
	vt.Write([]byte("\x1b[2J"))

	// Verify no adjacent duplicates
	for i := 1; i < len(vt.Scrollback); i++ {
		prev := lineText(vt.Scrollback[i-1])
		cur := lineText(vt.Scrollback[i])
		if prev == cur && prev != "" {
			t.Errorf("duplicate adjacent scrollback at index %d: %q", i, cur)
			dumpScrollback(t, vt)
			return
		}
	}
}

func TestResizeGrowReservesTrackedCaptureEndOffset(t *testing.T) {
	vt := New(5, 3)
	vt.AllowAltScreenScrollback = true
	vt.AltScreen = true

	makeLine := func(text string) []Cell {
		line := MakeBlankLine(5)
		for i, r := range text {
			if i >= 5 {
				break
			}
			line[i] = Cell{Rune: r, Width: 1}
		}
		return line
	}

	vt.Scrollback = append(vt.Scrollback,
		makeLine("hist1"),
		makeLine("hist2"),
		makeLine("cap1"),
		makeLine("cap2"),
		makeLine("cap3"),
		makeLine("tail"),
	)
	vt.altScreenCaptureLen = 3
	vt.altScreenCaptureDropLen = 3
	vt.altScreenCaptureTracked = true
	vt.altScreenCaptureEndOffset = 1

	vt.Resize(5, 5)

	if got := lineText(vt.Screen[0]); got != "hist1" {
		t.Fatalf("screen[0] = %q, want hist1", got)
	}
	if got := lineText(vt.Screen[1]); got != "hist2" {
		t.Fatalf("screen[1] = %q, want hist2", got)
	}
	if got := lineText(vt.Screen[2]); got != "" {
		t.Fatalf("expected reserved capture/tail rows to stay off-screen, got %q", got)
	}
}

func TestScrollUpCustomScrollRegionPreservesTrackedAltScreenCaptureReplacement(t *testing.T) {
	vt := New(10, 4)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	makeLine := func(text string) []Cell {
		line := MakeBlankLine(10)
		for i, r := range text {
			if i >= 10 {
				break
			}
			line[i] = Cell{Rune: r, Width: 1}
		}
		return line
	}

	vt.Screen[0] = makeLine("one")
	vt.Screen[1] = makeLine("two")
	vt.captureScreenToScrollback()

	vt.Screen[0] = makeLine("status")
	vt.Screen[1] = makeLine("one")
	vt.Screen[2] = makeLine("two")
	vt.Screen[3] = makeLine("three")
	vt.ScrollTop = 1
	vt.ScrollBottom = 4
	vt.scrollUp(1)
	vt.Screen[3] = makeLine("four")

	if vt.altScreenCaptureEndOffset != 1 {
		t.Fatalf("endOffset = %d, want 1 after region scroll", vt.altScreenCaptureEndOffset)
	}

	vt.captureScreenToScrollback()

	want := []string{"one", "status", "two", "three", "four"}
	if len(vt.Scrollback) != len(want) {
		dumpScrollback(t, vt)
		t.Fatalf("scrollback length = %d, want %d", len(vt.Scrollback), len(want))
	}
	for i, w := range want {
		if got := lineText(vt.Scrollback[i]); got != w {
			dumpScrollback(t, vt)
			t.Fatalf("scrollback[%d] = %q, want %q", i, got, w)
		}
	}
}

func TestAltScreenDropRecaptureResetsEndOffset(t *testing.T) {
	// Regression: dropTrackedAltScreenCapture must zero altScreenCaptureEndOffset
	// after dedup so the next capture starts with a clean offset. Without the fix,
	// stale endOffset from cycle 1 causes captureStart miscalculation in cycle 3.
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Cycle 1: AAA,BBB scroll off; CCC,DDD,EEE captured
	vt.Write([]byte("AAA\r\nBBB\r\nCCC\r\nDDD\r\nEEE"))
	vt.Write([]byte("\x1b[2J"))

	if vt.altScreenCaptureLen != 3 {
		t.Fatalf("cycle 1: captureLen = %d, want 3", vt.altScreenCaptureLen)
	}

	// Cycle 2: completely different content — forces drop+recapture
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("PPP\r\nQQQ\r\nRRR\r\nSSS\r\nTTT"))
	vt.Write([]byte("\x1b[2J"))

	if vt.altScreenCaptureEndOffset != 0 {
		t.Fatalf("cycle 2: endOffset = %d after drop+recapture, want 0",
			vt.altScreenCaptureEndOffset)
	}

	// Cycle 3: same as cycle 2 — should match cleanly
	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("PPP\r\nQQQ\r\nRRR\r\nSSS\r\nTTT"))
	vt.Write([]byte("\x1b[2J"))

	want := []string{"AAA", "BBB", "PPP", "QQQ", "RRR", "SSS", "TTT"}
	if len(vt.Scrollback) != len(want) {
		dumpScrollback(t, vt)
		t.Fatalf("scrollback length = %d, want %d", len(vt.Scrollback), len(want))
	}
	for i, w := range want {
		got := lineText(vt.Scrollback[i])
		if got != w {
			t.Errorf("scrollback[%d] = %q, want %q", i, got, w)
		}
	}
}
