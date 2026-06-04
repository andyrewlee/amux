package vterm

import "testing"

func TestLoadPaneCapture_RestoresVisibleScreenAndScrollback(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected scrollback to start with history, got %q", got)
	}
	if got := vt.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected first visible row to start with screen data, got %q", got)
	}
	if got := vt.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected second visible row to start with screen data, got %q", got)
	}
}

func TestLoadPaneCapture_RestoresCurrentStyle(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("\x1b[1;31mstyled prompt"))

	if !vt.CurrentStyle.Bold {
		t.Fatal("expected CurrentStyle.Bold to remain active after pane restore")
	}
	if vt.CurrentStyle.Fg.Type != ColorIndexed || vt.CurrentStyle.Fg.Value != 1 {
		t.Fatalf("expected CurrentStyle foreground to remain red, got type=%v value=%d", vt.CurrentStyle.Fg.Type, vt.CurrentStyle.Fg.Value)
	}
}

func TestLoadPaneCapture_RestoresCursorFromCaptureWhenMetadataUnavailable(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.CursorX = 7
	vt.CursorY = 1

	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	if vt.CursorX != 10 || vt.CursorY != 1 {
		t.Fatalf("expected cursor to follow the restored frame when explicit metadata is unavailable, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCaptureWithCursor_RestoresExplicitCursor(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)

	vt.LoadPaneCaptureWithCursorAndModes([]byte("history\nscreen one\nscreen two\n"), 5, 1, true, PaneModeState{PreserveExistingState: true})

	if vt.CursorX != 5 || vt.CursorY != 1 {
		t.Fatalf("expected explicit pane cursor to be restored, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCapture_ExitsSynchronizedOutput(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.Write([]byte("stale frame"))
	vt.setSynchronizedOutput(true)
	vt.Write([]byte("\nignored while frozen"))

	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	if vt.SyncActive() {
		t.Fatal("expected synchronized output to be disabled after pane restore")
	}
	screen, _ := vt.RenderBuffers()
	if got := screen[0][0].Rune; got != 's' {
		t.Fatalf("expected restored frame to be visible after sync reset, got %q", got)
	}
}

func TestLoadPaneCapture_ReplacesAltScreenScrollback(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	line := MakeBlankLine(20)
	line[0] = Cell{Rune: 'h', Width: 1}
	vt.Scrollback = append(vt.Scrollback, line)
	vt.enterAltScreen()

	vt.LoadPaneCapture([]byte("screen one\nscreen two\n"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected full pane restore to replace stale alt-screen scrollback, got %d lines", len(vt.Scrollback))
	}
}

func TestLoadPaneCapture_EmptyCaptureClearsFrameAndParserState(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.Write([]byte("stale"))
	vt.Write([]byte("\x1b["))

	vt.LoadPaneCaptureWithCursorAndModes(nil, 3, 0, true, PaneModeState{PreserveExistingState: true})

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty authoritative snapshot to clear scrollback, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected first cell to be blank after empty full restore, got %q", got)
	}
	if vt.ParserCarryState() != (ParserCarryState{}) {
		t.Fatalf("expected parser carry reset after full restore, got %+v", vt.ParserCarryState())
	}
	if vt.CursorX != 3 || vt.CursorY != 0 {
		t.Fatalf("expected explicit cursor restored on empty full capture, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCapture_EmptyCaptureWithoutCursorMetadataResetsCursorToOrigin(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	vt.CursorX = 11
	vt.CursorY = 1

	vt.LoadPaneCapture(nil)

	if vt.CursorX != 0 || vt.CursorY != 0 {
		t.Fatalf("expected empty authoritative snapshot without cursor metadata to reset stale cursor, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCapture_EmptyCaptureClearsAltScreenScrollback(t *testing.T) {
	t.Parallel()
	vt := New(20, 2)
	line := MakeBlankLine(20)
	line[0] = Cell{Rune: 'h', Width: 1}
	vt.Scrollback = append(vt.Scrollback, line)
	vt.enterAltScreen()
	vt.Write([]byte("stale"))

	vt.LoadPaneCaptureWithCursorAndModes(nil, 0, 0, true, PaneModeState{PreserveExistingState: true})

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty authoritative snapshot to clear stale alt-screen scrollback, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected empty authoritative snapshot to blank the visible screen, got %q", got)
	}
}

func TestLoadPaneCapture_ResetsTerminalModes(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	vt.enterAltScreen()
	vt.altScreenBuf[0][0] = Cell{Rune: 'x', Width: 1}
	vt.ScrollTop = 1
	vt.ScrollBottom = 2
	vt.OriginMode = true
	vt.CursorHidden = true
	vt.SavedCursorX = 5
	vt.SavedCursorY = 2
	vt.SavedStyle = Style{Bold: true}

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("\x1b[32mone\ntwo\nthree"),
		4,
		2,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    false,
			OriginMode:   false,
			CursorHidden: false,
			ScrollTop:    0,
			ScrollBottom: vt.Height,
		},
	)

	if vt.AltScreen {
		t.Fatal("expected full pane restore to leave alt screen mode")
	}
	if vt.altScreenBuf != nil {
		t.Fatal("expected alt screen buffer cleared after full pane restore")
	}
	if vt.ScrollTop != 0 || vt.ScrollBottom != vt.Height {
		t.Fatalf("expected full pane restore to reset scroll region, got top=%d bottom=%d height=%d", vt.ScrollTop, vt.ScrollBottom, vt.Height)
	}
	if vt.OriginMode {
		t.Fatal("expected origin mode reset after full pane restore")
	}
	if vt.CursorHidden {
		t.Fatal("expected cursor visibility mode reset after full pane restore")
	}
	if vt.SavedCursorX != vt.CursorX || vt.SavedCursorY != vt.CursorY {
		t.Fatalf("expected saved cursor to sync to restored cursor, got saved=(%d,%d) cursor=(%d,%d)", vt.SavedCursorX, vt.SavedCursorY, vt.CursorX, vt.CursorY)
	}
	if vt.SavedStyle != vt.CurrentStyle {
		t.Fatalf("expected saved style to sync to restored style, got saved=%+v current=%+v", vt.SavedStyle, vt.CurrentStyle)
	}
}

func TestLoadPaneCapture_ResetModesAffectSubsequentWrites(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	vt.enterAltScreen()
	vt.ScrollTop = 1
	vt.ScrollBottom = 2
	vt.OriginMode = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("one\ntwo\nthree"),
		0,
		2,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    false,
			OriginMode:   false,
			CursorHidden: false,
			ScrollTop:    0,
			ScrollBottom: vt.Height,
		},
	)
	vt.Write([]byte("\n"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected newline after restore to scroll full screen, got %d scrollback lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected top row to scroll into history after restore, got %q", got)
	}
	if got := vt.Screen[0][0].Rune; got != 't' {
		t.Fatalf("expected second row to become first visible row after restore scroll, got %q", got)
	}
}

func TestIsBlankLine(t *testing.T) {
	t.Parallel()
	blank := MakeBlankLine(10)
	if !isBlankLine(blank) {
		t.Error("MakeBlankLine should produce a blank line")
	}

	nonBlank := MakeBlankLine(10)
	nonBlank[5] = Cell{Rune: 'x', Width: 1}
	if isBlankLine(nonBlank) {
		t.Error("line with 'x' should not be blank")
	}

	// Null rune cells should be considered blank
	nullLine := make([]Cell, 10)
	if !isBlankLine(nullLine) {
		t.Error("line with null runes should be blank")
	}
}
