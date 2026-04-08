package common

import (
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestRestorePaneCapture_ViewportGrowthCountsOnlyRevealedHistoryRows(t *testing.T) {
	term := vterm.New(20, 2)

	RestorePaneCapture(
		term,
		[]byte("history\nscreen one\nscreen two\n"),
		[]byte("older\nhistory\n"),
		0,
		0,
		false,
		tmux.PaneModeState{},
		20,
		2,
		20,
		5,
	)

	if len(term.Scrollback) != 1 {
		t.Fatalf("expected only the truly new history row to be appended after viewport growth, got %d rows", len(term.Scrollback))
	}
	if got := term.Scrollback[0][0].Rune; got != 'o' {
		t.Fatalf("expected older row to be preserved in scrollback, got %q", got)
	}
	if got := term.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected the one revealed history row to stay on-screen, got %q", got)
	}
}

func TestRestorePaneCapture_DropsStaleParserCarryBeforeNewClientRedraw(t *testing.T) {
	term := vterm.New(20, 2)
	term.Write([]byte("\x1b"))

	RestorePaneCapture(
		term,
		[]byte("history\nscreen one\nscreen two\n"),
		nil,
		0,
		0,
		false,
		tmux.PaneModeState{},
		20,
		2,
		20,
		2,
	)

	if got := term.ParserCarryState(); got != (vterm.ParserCarryState{}) {
		t.Fatalf("expected full-pane restore to drop stale parser carry before the new tmux client attaches, got %+v", got)
	}

	term.Write([]byte("\x1b[2Jvisible"))
	if got := term.Screen[1][10].Rune; got != 'v' {
		t.Fatalf("expected new tmux client redraw to start cleanly at the restored cursor, got %q", got)
	}
	if got := term.Screen[1][11].Rune; got != 'i' {
		t.Fatalf("expected first redraw text to begin immediately after the clear-screen sequence, got %q", got)
	}
}

func TestRestorePaneCapture_DoesNotDuplicateHistoryWhenViewportShrinks(t *testing.T) {
	term := vterm.New(20, 2)

	RestorePaneCapture(
		term,
		[]byte("history\nscreen zero\nscreen one\nscreen two\n"),
		[]byte("history\nscreen zero\n"),
		0,
		0,
		false,
		tmux.PaneModeState{},
		20,
		3,
		20,
		2,
	)

	if term.Width != 20 || term.Height != 2 {
		t.Fatalf("expected restored terminal resized to the live viewport, got %dx%d", term.Width, term.Height)
	}
	if len(term.Scrollback) != 2 {
		t.Fatalf("expected resize-before-delta reconciliation to avoid duplicate history rows, got %d", len(term.Scrollback))
	}
	if got := term.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected original history row to remain first, got %q", got)
	}
	if got := term.Scrollback[1][0].Rune; got != 's' {
		t.Fatalf("expected resized-off row to be retained exactly once, got %q", got)
	}
	if got := term.Screen[0][0].Rune; got != 's' {
		t.Fatalf("expected live viewport top row to remain the restored middle row, got %q", got)
	}
	if got := term.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected live viewport bottom row to remain the restored tail row, got %q", got)
	}
}

func TestRestorePaneCapture_DoesNotDuplicateHistoryWhenViewportGrows(t *testing.T) {
	term := vterm.New(20, 2)

	RestorePaneCapture(
		term,
		[]byte("history\nscreen one\nscreen two\n"),
		[]byte("history\nscreen one\n"),
		0,
		0,
		false,
		tmux.PaneModeState{},
		20,
		2,
		20,
		3,
	)

	if term.Width != 20 || term.Height != 3 {
		t.Fatalf("expected restored terminal resized to the larger live viewport, got %dx%d", term.Width, term.Height)
	}
	if len(term.Scrollback) != 0 {
		t.Fatalf("expected grown viewport to avoid duplicating rows that remain visible, got %d", len(term.Scrollback))
	}
	if got := term.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected revealed history row to stay visible after grow, got %q", got)
	}
}

func TestRestorePaneCapture_DoesNotDuplicateVisibleTailWhenResizeChangesDuringAttach(t *testing.T) {
	term := vterm.New(20, 2)

	RestorePaneCapture(
		term,
		[]byte("history\nscreen one\nscreen two\n"),
		[]byte("history\nscreen one\nscreen two\n"),
		0,
		0,
		false,
		tmux.PaneModeState{},
		20,
		2,
		20,
		3,
	)

	if len(term.Scrollback) != 0 {
		t.Fatalf("expected visible post-attach rows to stay out of scrollback after resize, got %d", len(term.Scrollback))
	}
	if got := term.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected resized screen to keep the revealed history row visible, got %q", got)
	}
	if got := term.Screen[1][0].Rune; got != 's' {
		t.Fatalf("expected first snapshot row to remain on-screen after resize, got %q", got)
	}
}

func TestRestorePaneCapture_ReconcilesDeltaAtSnapshotWidthAfterResize(t *testing.T) {
	term := vterm.New(8, 2)

	RestorePaneCapture(
		term,
		[]byte("history\n12345678\nabcdefgh\n"),
		[]byte("history\n12345678\n"),
		0,
		0,
		false,
		tmux.PaneModeState{},
		8,
		2,
		4,
		2,
	)

	if term.Width != 4 || term.Height != 2 {
		t.Fatalf("expected restore resized to the narrower live viewport, got %dx%d", term.Width, term.Height)
	}
	if len(term.Scrollback) != 1 {
		t.Fatalf("expected width-change reconciliation to avoid duplicating the still-visible full-width row, got %d", len(term.Scrollback))
	}
	if got := term.Scrollback[0][0].Rune; got != 'h' {
		t.Fatalf("expected oldest history row to remain first after width change, got %q", got)
	}
}

func TestRestorePaneCapture_AltScreenViewportGrowthDoesNotRevealHistory(t *testing.T) {
	term := vterm.New(8, 2)

	RestorePaneCapture(
		term,
		[]byte("older\nmenu one\nmenu two\n"),
		nil,
		0,
		1,
		true,
		tmux.PaneModeState{
			HasState:     true,
			AltScreen:    true,
			ScrollTop:    0,
			ScrollBottom: 2,
		},
		8,
		2,
		8,
		4,
	)

	if !term.AltScreen {
		t.Fatal("expected restored pane to remain in alt-screen mode")
	}
	if len(term.Scrollback) != 1 {
		t.Fatalf("expected pre-alt history to stay in scrollback, got %d rows", len(term.Scrollback))
	}
	if got := term.Screen[0][0].Rune; got != 'm' {
		t.Fatalf("expected restored fullscreen content to stay at the top after grow, got %q", got)
	}
	if got := term.Screen[1][0].Rune; got != 'm' {
		t.Fatalf("expected second restored fullscreen row to remain visible after grow, got %q", got)
	}
	if got := term.Screen[2][0].Rune; got != ' ' {
		t.Fatalf("expected newly revealed rows to stay blank instead of revealing history, got %q", got)
	}
	if got := term.Screen[3][0].Rune; got != ' ' {
		t.Fatalf("expected bottom grown row to stay blank instead of revealing history, got %q", got)
	}
}

func TestRestoreScrollbackCapture_ScrollsToBottomBeforePrependingHistory(t *testing.T) {
	term := vterm.New(20, 2)
	term.LoadPaneCapture([]byte("oldest\nolder\nscreen one\nscreen two\n"))
	term.ScrollViewToTop()
	if term.ViewOffset == 0 {
		t.Fatal("expected seeded terminal to start scrolled into history")
	}

	RestoreScrollbackCapture(term, []byte("ancient\n"), 20, 1, 20, 2)

	if term.ViewOffset != 0 {
		t.Fatalf("expected history-only restore to land at live bottom, got view offset %d", term.ViewOffset)
	}
	if got := captureRestoreRowText(term.Scrollback[0], 20); got[:7] != "ancient" {
		t.Fatalf("expected prepended history to remain intact, got %q", got)
	}
}

func TestRestoreScrollbackCapture_PreservesBlankTailRows(t *testing.T) {
	term := vterm.New(8, 2)

	RestoreScrollbackCapture(term, []byte("first\n\n"), 8, 4, 8, 2)

	if len(term.Scrollback) != 2 {
		t.Fatalf("expected explicit blank tail row to survive history-only restore, got %d rows", len(term.Scrollback))
	}
	if got := term.Scrollback[0][0].Rune; got != 'f' {
		t.Fatalf("expected first restored row to contain captured text, got %q", got)
	}
	if !isBlankRestoreRow(term.Scrollback[1]) {
		t.Fatal("expected second restored row to remain a blank history row")
	}
}

func captureRestoreRowText(row []vterm.Cell, width int) string {
	runes := make([]rune, 0, width)
	for i := 0; i < width && i < len(row); i++ {
		r := row[i].Rune
		if r == 0 {
			r = ' '
		}
		runes = append(runes, r)
	}
	return string(runes)
}

func isBlankRestoreRow(row []vterm.Cell) bool {
	for _, cell := range row {
		if cell.Rune != 0 && cell.Rune != ' ' {
			return false
		}
	}
	return true
}
