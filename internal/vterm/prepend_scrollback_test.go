package vterm

import (
	"strings"
	"testing"
)

func TestPrependScrollbackEmpty(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback(nil)
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
	vt.PrependScrollback([]byte{})
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
}

func TestPrependScrollbackPlainText(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback([]byte("hello\nworld\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback to have lines")
	}

	// First line should start with 'h'
	if vt.Scrollback[0][0].Rune != 'h' {
		t.Errorf("expected 'h', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackPreservesExisting(t *testing.T) {
	vt := New(80, 24)

	// Add existing scrollback
	existing := MakeBlankLine(80)
	existing[0] = Cell{Rune: 'E', Width: 1}
	vt.Scrollback = append(vt.Scrollback, existing)

	vt.PrependScrollback([]byte("prepended\n"))

	// Should have at least 2 lines: prepended + existing
	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(vt.Scrollback))
	}

	// First line should be from prepended content
	if vt.Scrollback[0][0].Rune != 'p' {
		t.Errorf("first line should start with 'p', got %c", vt.Scrollback[0][0].Rune)
	}

	// Last line should be the original existing line
	last := vt.Scrollback[len(vt.Scrollback)-1]
	if last[0].Rune != 'E' {
		t.Errorf("last line should be existing 'E', got %c", last[0].Rune)
	}
}

func TestPrependScrollbackWithANSI(t *testing.T) {
	vt := New(80, 24)

	// Bold red text: ESC[1;31m
	vt.PrependScrollback([]byte("\x1b[1;31mred bold\x1b[0m\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines")
	}

	cell := vt.Scrollback[0][0]
	if cell.Rune != 'r' {
		t.Errorf("expected 'r', got %c", cell.Rune)
	}
	if !cell.Style.Bold {
		t.Error("expected bold style")
	}
	if cell.Style.Fg.Type != ColorIndexed || cell.Style.Fg.Value != 1 {
		t.Errorf("expected red foreground, got type=%v value=%d", cell.Style.Fg.Type, cell.Style.Fg.Value)
	}
}

func TestPrependScrollbackTrimsTrailingBlankLines(t *testing.T) {
	vt := New(80, 5)

	// One line of text followed by nothing -- the temp vterm will have
	// that text on screen row 0 and blank rows 1-4. Those trailing blanks
	// should be trimmed.
	vt.PrependScrollback([]byte("only line"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(vt.Scrollback))
	}
	if vt.Scrollback[0][0].Rune != 'o' {
		t.Errorf("expected 'o', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackLargeContent(t *testing.T) {
	vt := New(80, 24)

	// Generate content that exceeds the screen height to produce scrollback
	// in the temporary vterm.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	content := strings.Join(lines, "\n") + "\n"

	vt.PrependScrollback([]byte(content))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines from large content")
	}
	// Should have captured all 50 lines (some in scrollback, some on screen)
	if len(vt.Scrollback) < 50 {
		t.Errorf("expected at least 50 scrollback lines, got %d", len(vt.Scrollback))
	}
}

func TestPrependScrollbackRespectsMaxScrollback(t *testing.T) {
	vt := New(80, 24)

	// Fill existing scrollback close to max
	for i := 0; i < MaxScrollback-5; i++ {
		vt.Scrollback = append(vt.Scrollback, MakeBlankLine(80))
	}

	// Prepend 100 more lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	vt.PrependScrollback([]byte(strings.Join(lines, "\n") + "\n"))

	if len(vt.Scrollback) > MaxScrollback {
		t.Errorf("scrollback exceeded max: %d > %d", len(vt.Scrollback), MaxScrollback)
	}
}

func TestPrependScrollbackAllBlankContent(t *testing.T) {
	vt := New(80, 24)
	// Feed whitespace-only content -- should be a no-op
	vt.PrependScrollback([]byte("   \n   \n"))
	// The spaces are non-blank (they're space runes), so this actually
	// produces lines. Pure empty lines are also real tmux history rows and
	// should be preserved now that history captures no longer trim blank tails.
	// Feed truly empty content
	vt2 := New(80, 24)
	vt2.PrependScrollback([]byte("\n\n\n"))
	if len(vt2.Scrollback) != 3 {
		t.Errorf("expected 3 blank scrollback lines for all-blank content, got %d", len(vt2.Scrollback))
	}
}

func TestPrependScrollbackTreatsCaptureLFAsAsRowSeparators(t *testing.T) {
	vt := New(20, 5)
	vt.TreatLFAsCRLF = false
	vt.PrependScrollback([]byte("abc\nx"))

	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(vt.Scrollback))
	}
	if got := vt.Scrollback[1][0].Rune; got != 'x' {
		t.Fatalf("expected second line to start with x at col 0, got %q", got)
	}
}

func TestAppendScrollbackDelta_AppendsMissingSuffix(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	vt.AppendScrollbackDelta([]byte("history\nscreen one\n"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected appended history suffix, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected existing history line to remain first, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected missing scrolled row to append into history, got %q", got)
	}
	if got := vt.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected visible frame to remain unchanged, got %q", got)
	}
}

func TestAppendScrollbackDelta_IgnoresMismatchedCapture(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	vt.AppendScrollbackDelta([]byte("other history\nscreen one\n"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected mismatched history capture to be ignored, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected original history to remain untouched, got %q", got)
	}
}

func TestAppendScrollbackDelta_MatchesRetainedSuffixAfterTrim(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("zero\none\ntwo\nthree\nscreen one\nscreen two\n"))
	vt.Scrollback = append([][]Cell(nil), vt.Scrollback[2:]...)

	vt.AppendScrollbackDelta([]byte("zero\none\ntwo\nthree\nscreen one\n"))

	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected retained suffix match to append only the missing row, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 't' {
		t.Fatalf("expected retained suffix to start with two, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 't' {
		t.Fatalf("expected retained suffix to keep three, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 's' {
		t.Fatalf("expected missing scrolled row to append after retained suffix, got %q", got)
	}
}

func TestAppendScrollbackDelta_IgnoresTrailingStyledBlankDifferences(t *testing.T) {
	vt := New(6, 1)
	line := MakeBlankLine(6)
	line[0] = Cell{Rune: 'x', Width: 1}
	for i := 1; i < len(line); i++ {
		line[i] = Cell{Rune: ' ', Width: 1, Style: Style{Reverse: true}}
	}
	vt.Scrollback = append(vt.Scrollback, line)

	vt.AppendScrollbackDelta([]byte("x\nnext\n"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected trailing styled blanks to be ignored during match, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[1][0].Rune; got != 'n' {
		t.Fatalf("expected later history row to append after relaxed trailing-space match, got %q", got)
	}
}

func TestAppendScrollbackDelta_PrefersMatchAlignedWithCurrentScreenPrefix(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("prompt\ncmd output\nprompt\n"))

	vt.AppendScrollbackDelta([]byte("prompt\ncmd output\nprompt\n"))

	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected repeated-row capture to append the missing middle and tail rows, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'p' {
		t.Fatalf("expected retained prompt to stay first, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 'c' {
		t.Fatalf("expected screen-aligned middle row to reconcile into history, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 'p' {
		t.Fatalf("expected repeated trailing prompt to reconcile after the middle row, got %q", got)
	}
}

func TestLoadPaneCapture_RestoresVisibleScreenAndScrollback(t *testing.T) {
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
	vt := New(20, 2)
	vt.CursorX = 7
	vt.CursorY = 1

	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))

	if vt.CursorX != 10 || vt.CursorY != 1 {
		t.Fatalf("expected cursor to follow the restored frame when explicit metadata is unavailable, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCaptureWithCursor_RestoresExplicitCursor(t *testing.T) {
	vt := New(20, 2)

	vt.LoadPaneCaptureWithCursor([]byte("history\nscreen one\nscreen two\n"), 5, 1, true)

	if vt.CursorX != 5 || vt.CursorY != 1 {
		t.Fatalf("expected explicit pane cursor to be restored, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCapture_ExitsSynchronizedOutput(t *testing.T) {
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
	vt := New(20, 2)
	vt.Write([]byte("stale"))
	vt.Write([]byte("\x1b["))

	vt.LoadPaneCaptureWithCursor(nil, 3, 0, true)

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
	vt := New(20, 2)
	vt.CursorX = 11
	vt.CursorY = 1

	vt.LoadPaneCapture(nil)

	if vt.CursorX != 0 || vt.CursorY != 0 {
		t.Fatalf("expected empty authoritative snapshot without cursor metadata to reset stale cursor, got (%d,%d)", vt.CursorX, vt.CursorY)
	}
}

func TestLoadPaneCapture_EmptyCaptureClearsAltScreenScrollback(t *testing.T) {
	vt := New(20, 2)
	line := MakeBlankLine(20)
	line[0] = Cell{Rune: 'h', Width: 1}
	vt.Scrollback = append(vt.Scrollback, line)
	vt.enterAltScreen()
	vt.Write([]byte("stale"))

	vt.LoadPaneCaptureWithCursor(nil, 0, 0, true)

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty authoritative snapshot to clear stale alt-screen scrollback, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected empty authoritative snapshot to blank the visible screen, got %q", got)
	}
}

func TestLoadPaneCapture_ResetsTerminalModes(t *testing.T) {
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
