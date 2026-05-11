package vterm

import "testing"

func TestSelectionMappingUsesSyncBuffers(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.Screen = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}

	vt.setSynchronizedOutput(true)

	// Grow scrollback after sync to simulate output while frozen.
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	if got := vt.ScreenYToAbsoluteLine(0); got != 2 {
		t.Fatalf("expected screenY 0 to map to abs line 2 with sync buffers, got %d", got)
	}
	if got := vt.AbsoluteLineToScreenY(2); got != 0 {
		t.Fatalf("expected abs line 2 to map to screenY 0 with sync buffers, got %d", got)
	}
	if got := vt.AbsoluteLineToScreenY(4); got != -1 {
		t.Fatalf("expected abs line 4 to be off-screen with sync buffers, got %d", got)
	}
}

func TestSyncScrollClampsToFrozenScrollback(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.Screen = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}

	vt.setSynchronizedOutput(true)

	// Grow live scrollback after sync begins; frozen history should remain at 2.
	vt.Scrollback = append(vt.Scrollback, MakeBlankLine(5), MakeBlankLine(5))

	vt.ScrollViewToTop()
	if offset, maxOffset := vt.GetScrollInfo(); offset != 2 || maxOffset != 2 {
		t.Fatalf("expected sync scroll top to clamp to frozen max 2, got offset=%d max=%d", offset, maxOffset)
	}

	vt.ScrollToLine(0)
	if offset, maxOffset := vt.GetScrollInfo(); offset != 2 || maxOffset != 2 {
		t.Fatalf("expected ScrollToLine to respect frozen sync buffers, got offset=%d max=%d", offset, maxOffset)
	}
}

func TestSyncScrollbackGrowthDoesNotShiftAnchoredView(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.Screen = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.ViewOffset = 1

	vt.setSynchronizedOutput(true)

	beforeLine := vt.ScreenYToAbsoluteLine(0)
	vt.scrollUp(1)

	if got := vt.ScreenYToAbsoluteLine(0); got != beforeLine {
		t.Fatalf("expected sync mapping to stay anchored while frozen, got %d want %d", got, beforeLine)
	}

	vt.setSynchronizedOutput(false)

	if got := vt.ScreenYToAbsoluteLine(0); got != beforeLine {
		t.Fatalf("expected sync exit to preserve anchored line %d, got %d", beforeLine, got)
	}
	if offset, maxOffset := vt.GetScrollInfo(); offset != 2 || maxOffset != 4 {
		t.Fatalf("expected sync exit to preserve live offset 2 with max 4, got offset=%d max=%d", offset, maxOffset)
	}
}

func TestSyncOutputKeepsLiveBottomWhenViewportNeverScrolled(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.Screen = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}

	vt.setSynchronizedOutput(true)
	vt.scrollUp(1)
	vt.setSynchronizedOutput(false)

	if offset, maxOffset := vt.GetScrollInfo(); offset != 0 || maxOffset != 4 {
		t.Fatalf("expected live-follow viewport to remain at bottom after sync, got offset=%d max=%d", offset, maxOffset)
	}
}

func TestSyncOutputDoesNotRestoreHistoryAfterUserReturnsToBottom(t *testing.T) {
	vt := New(5, 2)
	vt.Scrollback = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
		MakeBlankLine(5),
	}
	vt.Screen = [][]Cell{
		MakeBlankLine(5),
		MakeBlankLine(5),
	}

	vt.setSynchronizedOutput(true)
	vt.ScrollView(1)
	vt.scrollUp(1)
	vt.ScrollViewToBottom()
	vt.setSynchronizedOutput(false)

	if offset, maxOffset := vt.GetScrollInfo(); offset != 0 || maxOffset != 4 {
		t.Fatalf("expected viewport to stay at bottom after sync exit, got offset=%d max=%d", offset, maxOffset)
	}
}
