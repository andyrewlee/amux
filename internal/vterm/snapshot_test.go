package vterm

import "testing"

// Tests for the TerminalSnapshot convenience wrappers in snapshot.go.
//
// AppendDelta and PrependHistory are thin adapters over
// AppendScrollbackDeltaWithSize / PrependScrollbackWithSize. These tests assert
// that the wrappers forward every snapshot field (Data, Cols, Rows) faithfully
// and that the documented zero-geometry fallback and empty-input no-op contracts
// hold. LoadSnapshot already has dedicated coverage in
// prepend_scrollback_load_test.go, so it is not re-tested here.

func TestAppendDelta_NoOpForEmptyData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil data", data: nil},
		{name: "empty data", data: []byte{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(20, 2)
			vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))
			before := len(vt.Scrollback)

			vt.AppendDelta(TerminalSnapshot{Data: tc.data, Cols: 20, Rows: 2}, 0)

			if got := len(vt.Scrollback); got != before {
				t.Fatalf("expected empty delta to be a no-op, scrollback went %d -> %d", before, got)
			}
		})
	}
}

func TestAppendDelta_AppendsMissingSuffix(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	// The newer capture lost "screen two" off the bottom of the visible frame,
	// so "screen one" should reconcile into history as the missing suffix.
	vt.AppendDelta(TerminalSnapshot{
		Data: []byte("history\nscreen one\n"),
		Cols: 20,
		Rows: 2,
	}, 0)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected appended history suffix, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "history" {
		t.Fatalf("expected existing history line to remain first, got %q", got)
	}
	if got := plainLine(vt.Scrollback[1]); got != "screen one" {
		t.Fatalf("expected missing scrolled row to append into history, got %q", got)
	}
	if got := plainLine(vt.Screen[0]); got != "screen one" {
		t.Fatalf("expected visible frame to remain unchanged, got %q", got)
	}
}

func TestAppendDelta_IgnoresMismatchedCapture(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	// A capture whose retained suffix does not align with current scrollback is
	// rejected wholesale rather than appended.
	vt.AppendDelta(TerminalSnapshot{
		Data: []byte("other history\nscreen one\n"),
		Cols: 20,
		Rows: 2,
	}, 0)

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected mismatched history capture to be ignored, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "history" {
		t.Fatalf("expected original history to remain untouched, got %q", got)
	}
}

func TestAppendDelta_ZeroGeometryFallsBackToTerminalSize(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	// Cols/Rows left at zero must reuse the terminal's current 20x2 geometry so
	// the capture parses identically to the explicit-geometry case above.
	vt.AppendDelta(TerminalSnapshot{Data: []byte("history\nscreen one\n")}, 0)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected zero-geometry delta to fall back to terminal size and append, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[1]); got != "screen one" {
		t.Fatalf("expected missing row appended under fallback geometry, got %q", got)
	}
}

func TestAppendDelta_VisibleHistoryRowsTrimsTailFromAppend(t *testing.T) {
	t.Parallel()
	// Same-geometry exercise (capture and terminal both 20x2, no Resize) so the
	// resize-driven visible-tail auto-detection in AppendScrollbackDeltaWithSize
	// is bypassed and the visibleHistoryRows argument alone decides how many
	// trailing rows are held back from the append. The capture is the full frame
	// in both cases; only the parameter changes, which makes a positive
	// visibleHistoryRows load-bearing for the assertion rather than dead.
	//
	// For the contrast against TestAppendDelta_VisibleHistoryRowsAvoidDoubleAppend
	// below, see that test for the resize-driven auto-detection path where
	// visibleHistoryRows is overridden by the detected visible tail.
	tests := []struct {
		name               string
		visibleHistoryRows int
		wantGrowth         int
		wantNewTail        string
	}{
		{
			// Both screen rows are reconciled into history because none are held
			// back as still-visible.
			name:               "zero appends both missing screen rows",
			visibleHistoryRows: 0,
			wantGrowth:         2,
			wantNewTail:        "screen two",
		},
		{
			// The last captured row is reported as still on screen, so only the
			// row above it reconciles into history.
			name:               "one holds the visible tail row back",
			visibleHistoryRows: 1,
			wantGrowth:         1,
			wantNewTail:        "screen one",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(20, 2)
			vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))
			before := len(vt.Scrollback)

			vt.AppendDelta(TerminalSnapshot{
				Data: []byte("history\nscreen one\nscreen two\n"),
				Cols: 20,
				Rows: 2,
			}, tc.visibleHistoryRows)

			if got := len(vt.Scrollback) - before; got != tc.wantGrowth {
				t.Fatalf("expected scrollback to grow by %d with visibleHistoryRows=%d, grew by %d",
					tc.wantGrowth, tc.visibleHistoryRows, got)
			}
			if got := plainLine(vt.Scrollback[len(vt.Scrollback)-1]); got != tc.wantNewTail {
				t.Fatalf("expected newest reconciled history row %q, got %q", tc.wantNewTail, got)
			}
		})
	}
}

func TestAppendDelta_VisibleHistoryRowsAvoidDoubleAppend(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))
	// A resize folded the captured "history" row back onto the visible screen.
	vt.Resize(20, 3)

	// With a geometry mismatch (capture 20x2 vs terminal 20x3) the resize-driven
	// visible-tail auto-detection runs and overrides visibleHistoryRows, so the
	// argument is not load-bearing here. The same-geometry test above is what
	// exercises the parameter directly; this case covers the auto-detection path:
	// the row already revealed on screen must not be pushed back into scrollback.
	vt.AppendDelta(TerminalSnapshot{
		Data: []byte("history\nscreen one\nscreen two\n"),
		Cols: 20,
		Rows: 2,
	}, 1)

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected rows still visible after growth to stay off scrollback, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Screen[0]); got != "history" {
		t.Fatalf("expected grown viewport to keep the revealed history row visible, got %q", got)
	}
}

func TestAppendDelta_NegativeVisibleHistoryRowsClampedToZero(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	// A negative count is clamped to zero by the underlying impl, so the missing
	// suffix still reconciles exactly as in the zero case.
	vt.AppendDelta(TerminalSnapshot{
		Data: []byte("history\nscreen one\n"),
		Cols: 20,
		Rows: 2,
	}, -5)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected negative visibleHistoryRows clamped to zero, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[1]); got != "screen one" {
		t.Fatalf("expected missing row appended despite negative clamp, got %q", got)
	}
}

func TestAppendDelta_ForwardsSnapshotWidthSoRowsDoNotWrap(t *testing.T) {
	t.Parallel()
	// The terminal is only 3 wide, but the capture was taken at 5 wide. Seed
	// scrollback with an intact 5-wide row so the delta's retained-suffix match
	// has an anchor, then forward Cols=5 on the delta. If AppendDelta forwarded
	// the terminal's 3-wide geometry instead, the captured rows would wrap
	// ("abcde" -> "abc","de") and the retained 5-wide seed "abcde" would match
	// no wrapped line, so the delta would append nothing and scrollback would
	// stay at 1 line.
	vt := New(3, 1)
	seed := MakeBlankLine(5)
	for i, r := range "abcde" {
		seed[i] = Cell{Rune: r, Width: 1}
	}
	vt.Scrollback = append(vt.Scrollback, seed)

	vt.AppendDelta(TerminalSnapshot{
		Data: []byte("abcde\nfghij\n"),
		Cols: 5,
		Rows: 1,
	}, 0)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected the one missing row appended under forwarded 5-wide geometry, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "abcde" {
		t.Fatalf("expected seeded 5-wide row retained, got %q", got)
	}
	if got := plainLine(vt.Scrollback[1]); got != "fghij" {
		t.Fatalf("expected second row appended intact at capture width (not wrapped), got %q", got)
	}
}

func TestPrependHistory_NoOpForEmptyData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil data", data: nil},
		{name: "empty data", data: []byte{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(20, 2)
			existing := MakeBlankLine(20)
			existing[0] = Cell{Rune: 'E', Width: 1}
			vt.Scrollback = append(vt.Scrollback, existing)

			vt.PrependHistory(TerminalSnapshot{Data: tc.data, Cols: 20, Rows: 2})

			if len(vt.Scrollback) != 1 {
				t.Fatalf("expected empty prepend to be a no-op, got %d lines", len(vt.Scrollback))
			}
			if got := plainLine(vt.Scrollback[0]); got != "E" {
				t.Fatalf("expected existing scrollback untouched, got %q", got)
			}
		})
	}
}

func TestPrependHistory_InsertsAboveExistingScrollback(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	existing := MakeBlankLine(20)
	existing[0] = Cell{Rune: 'E', Width: 1}
	vt.Scrollback = append(vt.Scrollback, existing)

	vt.PrependHistory(TerminalSnapshot{
		Data: []byte("older one\nolder two\n"),
		Cols: 20,
		Rows: 2,
	})

	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected prepended history plus existing line, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "older one" {
		t.Fatalf("expected oldest prepended row first, got %q", got)
	}
	if got := plainLine(vt.Scrollback[1]); got != "older two" {
		t.Fatalf("expected second prepended row in order, got %q", got)
	}
	if got := plainLine(vt.Scrollback[len(vt.Scrollback)-1]); got != "E" {
		t.Fatalf("expected pre-existing scrollback to remain last, got %q", got)
	}
}

func TestPrependHistory_ZeroGeometryFallsBackToTerminalSize(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)

	// Zero Cols/Rows must reuse the terminal's 20x2 geometry.
	vt.PrependHistory(TerminalSnapshot{Data: []byte("hello\nworld\n")})

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected two prepended rows under fallback geometry, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "hello" {
		t.Fatalf("expected first prepended row, got %q", got)
	}
	if got := plainLine(vt.Scrollback[1]); got != "world" {
		t.Fatalf("expected second prepended row, got %q", got)
	}
}

func TestPrependHistory_ForwardsSnapshotWidth(t *testing.T) {
	t.Parallel()
	// Terminal is only 3 wide; the capture was taken at 5 wide. Forwarding
	// Cols=5 must keep each 5-rune row intact instead of wrapping at the
	// terminal width.
	vt := New(3, 2)

	vt.PrependHistory(TerminalSnapshot{
		Data: []byte("abcde\n"),
		Cols: 5,
		Rows: 2,
	})

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected a single row at capture width, got %d lines", len(vt.Scrollback))
	}
	if got := plainLine(vt.Scrollback[0]); got != "abcde" {
		t.Fatalf("expected row parsed at forwarded 5-wide geometry, got %q", got)
	}
}

func TestPrependHistory_PreservesANSIStyling(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)

	vt.PrependHistory(TerminalSnapshot{
		Data: []byte("\x1b[1;31mred bold\x1b[0m\n"),
		Cols: 20,
		Rows: 2,
	})

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected prepended styled scrollback line")
	}
	cell := vt.Scrollback[0][0]
	if cell.Rune != 'r' {
		t.Fatalf("expected first rune 'r', got %q", cell.Rune)
	}
	if !cell.Style.Bold {
		t.Fatal("expected bold style preserved through PrependHistory")
	}
	if cell.Style.Fg.Type != ColorIndexed || cell.Style.Fg.Value != 1 {
		t.Fatalf("expected red foreground, got type=%v value=%d", cell.Style.Fg.Type, cell.Style.Fg.Value)
	}
}

func TestPrependHistory_RespectsMaxScrollback(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	for i := 0; i < MaxScrollback-5; i++ {
		vt.Scrollback = append(vt.Scrollback, MakeBlankLine(20))
	}

	var data []byte
	for i := 0; i < 100; i++ {
		data = append(data, []byte("line\n")...)
	}
	vt.PrependHistory(TerminalSnapshot{Data: data, Cols: 20, Rows: 2})

	if len(vt.Scrollback) > MaxScrollback {
		t.Fatalf("scrollback exceeded max: %d > %d", len(vt.Scrollback), MaxScrollback)
	}
}
