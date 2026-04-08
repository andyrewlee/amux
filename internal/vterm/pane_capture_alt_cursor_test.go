package vterm

import "testing"

func TestLoadPaneCaptureWithCursorAndModes_ClampsAltSavedCursorAfterResize(t *testing.T) {
	vt := New(10, 3)
	vt.Write([]byte("shell"))
	vt.enterAltScreen()

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("menu one\nmenu two\nmenu tri\n"),
		0,
		2,
		true,
		PaneModeState{
			HasState:          true,
			AltScreen:         true,
			ScrollTop:         0,
			ScrollBottom:      3,
			HasAltSavedCursor: true,
			AltSavedCursorX:   8,
			AltSavedCursorY:   2,
		},
	)

	vt.Resize(5, 2)

	if vt.altCursorX != 4 || vt.altCursorY != 1 {
		t.Fatalf("expected resize to clamp saved alt-screen cursor into the live viewport, got (%d,%d)", vt.altCursorX, vt.altCursorY)
	}

	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected 1049l to exit alt screen")
	}
	if vt.CursorX != 4 || vt.CursorY != 1 {
		t.Fatalf("expected 1049l to restore the clamped saved cursor, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}
