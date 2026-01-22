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
