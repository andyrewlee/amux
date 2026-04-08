package vterm

import "testing"

func TestLoadPaneCaptureWithCursorAndModes_PreservesActiveAltScreenBuffer(t *testing.T) {
	vt := New(6, 3)
	vt.Write([]byte("shell"))
	vt.enterAltScreen()
	vt.Write([]byte("tui"))
	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("one\ntwo\nthree"),
		0,
		2,
		true,
		PaneModeState{
			HasState:          true,
			AltScreen:         true,
			OriginMode:        true,
			CursorHidden:      true,
			ScrollTop:         1,
			ScrollBottom:      2,
			HasAltSavedCursor: true,
			AltSavedCursorX:   5,
			AltSavedCursorY:   1,
		},
	)

	if !vt.AltScreen {
		t.Fatal("expected pane mode restore to keep active alt screen mode")
	}
	if vt.altScreenBuf == nil {
		t.Fatal("expected pane mode restore to keep alt screen buffer")
	}
	if vt.ScrollTop != 1 || vt.ScrollBottom != 2 {
		t.Fatalf("expected pane mode restore to keep scroll region, got top=%d bottom=%d", vt.ScrollTop, vt.ScrollBottom)
	}
	if !vt.OriginMode {
		t.Fatal("expected pane mode restore to keep origin mode")
	}
	if !vt.CursorHidden {
		t.Fatal("expected pane mode restore to keep cursor visibility mode")
	}
	if vt.altCursorX != 5 || vt.altCursorY != 1 {
		t.Fatalf("expected alt-screen saved cursor to follow pane mode state, got (%d,%d)", vt.altCursorX, vt.altCursorY)
	}
	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected later 1049l to exit alt screen")
	}
	if got := vt.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected preserved main-screen buffer to be restorable, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_AppliesPaneModesToSubsequentWrites(t *testing.T) {
	vt := New(6, 4)
	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("head\nbody\nmid\nfoot"),
		0,
		2,
		true,
		PaneModeState{
			HasState:     true,
			OriginMode:   true,
			CursorHidden: true,
			ScrollTop:    1,
			ScrollBottom: 3,
		},
	)
	vt.Write([]byte("\n"))

	if vt.ScrollTop != 1 || vt.ScrollBottom != 3 {
		t.Fatalf("expected pane mode restore to keep scroll region, got top=%d bottom=%d", vt.ScrollTop, vt.ScrollBottom)
	}
	if !vt.OriginMode {
		t.Fatal("expected pane mode restore to keep origin mode")
	}
	if !vt.CursorHidden {
		t.Fatal("expected pane mode restore to keep cursor visibility mode")
	}
	if got := vt.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected pane-mode header row to stay fixed, got %q", got)
	}
	if got := vt.Screen[1][0].Rune; got != 'm' {
		t.Fatalf("expected newline to scroll only within pane-mode region, got %q", got)
	}
	if got := vt.Screen[2][0].Rune; got != ' ' {
		t.Fatalf("expected bottom row of pane-mode region to blank after scroll, got %q", got)
	}
	if got := vt.Screen[3][0].Rune; got != 'f' {
		t.Fatalf("expected pane-mode footer row to stay fixed, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_ClearsStaleAltScreenState(t *testing.T) {
	vt := New(8, 3)
	vt.Write([]byte("oldhome"))
	vt.enterAltScreen()
	vt.Write([]byte("oldtui"))
	vt.ScrollTop = 1
	vt.ScrollBottom = 2
	vt.OriginMode = true
	vt.CursorHidden = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("newhome\nprompt\n"),
		0,
		1,
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
		t.Fatal("expected main-screen snapshot to clear stale alt screen mode")
	}
	if vt.altScreenBuf != nil {
		t.Fatal("expected main-screen snapshot to drop stale alt screen buffer")
	}
	if vt.ScrollTop != 0 || vt.ScrollBottom != vt.Height {
		t.Fatalf("expected main-screen snapshot to reset scroll region, got top=%d bottom=%d", vt.ScrollTop, vt.ScrollBottom)
	}
	if vt.OriginMode {
		t.Fatal("expected main-screen snapshot to reset stale origin mode")
	}
	if vt.CursorHidden {
		t.Fatal("expected main-screen snapshot to reset stale cursor-hidden mode")
	}

	vt.Write([]byte("\x1b[?1049h\x1b[?1049l"))

	if got := vt.Screen[0][0].Rune; got != 'n' {
		t.Fatalf("expected later alt-screen cycle to restore current snapshot, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_FreshAltScreenSnapshotKeepsHistoryWithoutInventingMainBuffer(t *testing.T) {
	vt := New(8, 2)

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("older\nshell\nprompt\nmenu one\nmenu two\n"),
		0,
		1,
		true,
		PaneModeState{
			HasState:          true,
			AltScreen:         true,
			ScrollTop:         0,
			ScrollBottom:      2,
			HasAltSavedCursor: true,
			AltSavedCursorX:   6,
			AltSavedCursorY:   1,
		},
	)

	if !vt.AltScreen {
		t.Fatal("expected fresh snapshot restore to remain in alt screen mode")
	}
	if vt.altScreenBuf == nil {
		t.Fatal("expected fresh alt-screen snapshot to retain an alt-screen buffer")
	}
	if !isBlankScreen(vt.altScreenBuf) {
		t.Fatal("expected fresh alt-screen snapshot to leave the hidden main buffer blank when tmux did not provide one")
	}
	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected all pre-alt history to remain in scrollback, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected oldest history row to remain in scrollback, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected shell history row to remain in scrollback, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 'p' {
		t.Fatalf("expected prompt history row to remain in scrollback, got %q", got)
	}

	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected later 1049l to exit alt screen")
	}
	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected hidden main buffer to stay blank without authoritative data, got %q", got)
	}
	if got := vt.Screen[1][0].Rune; got != ' ' {
		t.Fatalf("expected blank fallback main screen after 1049l, got %q", got)
	}
	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected authoritative alt-screen restore to preserve scrollback after 1049l, got %d rows", len(vt.Scrollback))
	}
}

func TestLoadPaneCaptureWithCursorAndModes_FirstAltScreenRedrawDoesNotDuplicateRestoredFrame(t *testing.T) {
	vt := New(8, 2)
	vt.AllowAltScreenScrollback = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("older\nshell\nmenu one\nmenu two\n"),
		0,
		1,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 2,
		},
	)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected pre-alt history only after restore, got %d rows", len(vt.Scrollback))
	}

	vt.Write([]byte("\x1b[H\x1b[2J"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected first attach redraw clear to avoid duplicating restored frame, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected oldest history row to remain first after first redraw, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected shell history row to remain second after first redraw, got %q", got)
	}

	vt.Write([]byte("next one\r\nnext two"))
	vt.Write([]byte("\x1b[H\x1b[2J"))

	if len(vt.Scrollback) != 4 {
		t.Fatalf("expected later redraws to capture the live alt-screen frame once, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[2][0].Rune; got != 'n' {
		t.Fatalf("expected later redraw to capture the new frame, got %q", got)
	}
	if got := vt.Scrollback[3][0].Rune; got != 'n' {
		t.Fatalf("expected later redraw to capture the second new frame row, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_FirstAltScreenShiftAfterAttachPreservesScrolledOffTop(t *testing.T) {
	vt := New(8, 3)
	vt.AllowAltScreenScrollback = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("history\nline1\nline2\nline3\n"),
		0,
		2,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 3,
		},
	)

	vt.Write([]byte("\x1b[H\x1b[2J"))
	vt.Write([]byte("line2\r\nline3\r\nline4"))
	vt.Write([]byte("\x1b[H\x1b[2J"))

	want := []rune{'h', 'l', 'l', 'l', 'l'}
	if len(vt.Scrollback) != len(want) {
		t.Fatalf("expected restored-frame shift to preserve only the scrolled-off prefix, got %d rows", len(vt.Scrollback))
	}
	for i, wantRune := range want {
		if got := vt.Scrollback[i][0].Rune; got != wantRune {
			t.Fatalf("expected scrollback row %d to start with %q, got %q", i, wantRune, got)
		}
	}
}

func TestLoadPaneCaptureWithCursorAndModes_FirstAltScreenRedrawAfterResizeDoesNotDuplicateRestoredFrame(t *testing.T) {
	vt := New(8, 3)
	vt.AllowAltScreenScrollback = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("older\nmenu one\nmenu two\nmenu tre\n"),
		0,
		2,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 3,
		},
	)
	vt.Resize(8, 2)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected resize to move only the trimmed top row into history, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected oldest history row to remain first after resize, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 'm' {
		t.Fatalf("expected resized-off row to be retained before redraw, got %q", got)
	}

	vt.Write([]byte("\x1b[H\x1b[2J"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected first redraw after resize to avoid duplicating restored frame, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected oldest history row to remain first after redraw, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 'm' {
		t.Fatalf("expected resized-off row to remain second after redraw, got %q", got)
	}
}

func TestAppendScrollbackDeltaWithSize_PreservesPendingAltScreenDedupAfterHistoryAppend(t *testing.T) {
	vt := New(8, 2)
	vt.AllowAltScreenScrollback = true

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("older\nmenu one\nmenu two\n"),
		0,
		1,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 2,
		},
	)

	vt.AppendScrollbackDeltaWithSize([]byte("older\nmenu one\nmenu two\nmenu tre\n"), 8, 2, 0)
	vt.Write([]byte("\x1b[H\x1b[2J"))

	if len(vt.Scrollback) != 4 {
		t.Fatalf("expected post-attach history append to preserve the pending alt-screen dedupe marker, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected original history row to remain first, got %q", got)
	}
	if got := vt.Scrollback[3][0].Rune; got != 'm' {
		t.Fatalf("expected appended scrolled row to remain last after first redraw, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursor_PreservesExistingAltScreenStateWhenModesUnknown(t *testing.T) {
	vt := New(6, 3)
	vt.Write([]byte("shell"))
	vt.enterAltScreen()
	vt.Write([]byte("tui"))

	vt.LoadPaneCaptureWithCursor([]byte("one\ntwo\nthree"), 0, 2, true)

	if !vt.AltScreen {
		t.Fatal("expected capture without pane modes to preserve existing alt-screen state")
	}

	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected later 1049l to exit preserved alt screen")
	}
	if got := vt.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected preserved hidden main screen after unknown-mode restore, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_DoesNotReplaceExistingMainBufferWithHistoryTail(t *testing.T) {
	vt := New(8, 2)
	vt.Write([]byte("oldhome"))
	vt.enterAltScreen()
	vt.Write([]byte("oldtui"))

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("older\nshell\nprompt\nmenu one\nmenu two\n"),
		0,
		1,
		true,
		PaneModeState{
			HasState:          true,
			AltScreen:         true,
			ScrollTop:         0,
			ScrollBottom:      2,
			HasAltSavedCursor: true,
			AltSavedCursorX:   6,
			AltSavedCursorY:   1,
		},
	)

	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected 1049l to exit alt screen")
	}
	if got := vt.Screen[0][0].Rune; got != 'o' {
		t.Fatalf("expected existing hidden main screen to survive alt-screen restore, got %q", got)
	}
	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected ordinary scrollback history to stay intact, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected oldest history row to remain in scrollback, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected shell history row to remain in scrollback, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 'p' {
		t.Fatalf("expected prompt history row to remain in scrollback, got %q", got)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_PreservesExistingAltSavedCursorWhenOmitted(t *testing.T) {
	vt := New(8, 3)
	vt.Write([]byte("shell"))
	vt.enterAltScreen()
	vt.altCursorX = 4
	vt.altCursorY = 1
	vt.Write([]byte("menu"))

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("menu one\nmenu two\nmenu tri\n"),
		0,
		2,
		true,
		PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 3,
		},
	)

	if vt.altCursorX != 4 || vt.altCursorY != 1 {
		t.Fatalf("expected omitted alt-screen saved cursor to preserve existing value, got (%d,%d)", vt.altCursorX, vt.altCursorY)
	}

	vt.Write([]byte("\x1b[?1049l"))

	if vt.AltScreen {
		t.Fatal("expected later 1049l to exit alt screen")
	}
	if vt.CursorX != 4 || vt.CursorY != 1 {
		t.Fatalf("expected 1049l to restore the preserved saved cursor, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCaptureWithCursorAndModes_ResetsScrollRegionWhenOmitted(t *testing.T) {
	vt := New(6, 4)
	vt.ScrollTop = 1
	vt.ScrollBottom = 3

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("head\nbody\nmid\nfoot"),
		0,
		2,
		true,
		PaneModeState{
			HasState: true,
		},
	)

	if vt.ScrollTop != 0 || vt.ScrollBottom != vt.Height {
		t.Fatalf("expected omitted scroll-region metadata to reset to the full viewport, got top=%d bottom=%d", vt.ScrollTop, vt.ScrollBottom)
	}

	vt.Write([]byte("\n"))

	if got := vt.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected top row to stay fixed after resetting to the full viewport, got %q", got)
	}
	if got := vt.Screen[1][0].Rune; got != 'b' {
		t.Fatalf("expected stale subregion scroll to stay inactive after reset, got %q", got)
	}
	if got := vt.Screen[2][0].Rune; got != 'm' {
		t.Fatalf("expected cursor row contents to remain unchanged without stale-region scroll, got %q", got)
	}
	if got := vt.Screen[3][0].Rune; got != 'f' {
		t.Fatalf("expected footer row to remain in place without stale-region scroll, got %q", got)
	}
}
