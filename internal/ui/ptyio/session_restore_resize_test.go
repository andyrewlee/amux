package ptyio

import (
	"reflect"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestResizeTerminalForSessionRestore(t *testing.T) {
	tests := []struct {
		name        string
		startW      int
		startH      int
		cols        int
		rows        int
		wantW       int
		wantH       int
		wantNoTouch bool // when true the terminal must keep its starting dimensions
	}{
		{
			name:   "grows reused terminal to larger pty",
			startW: 20, startH: 4,
			cols: 40, rows: 10,
			wantW: 40, wantH: 10,
		},
		{
			name:   "shrinks reused terminal to smaller pty",
			startW: 40, startH: 10,
			cols: 20, rows: 4,
			wantW: 20, wantH: 4,
		},
		{
			name:   "width only change resizes",
			startW: 20, startH: 4,
			cols: 30, rows: 4,
			wantW: 30, wantH: 4,
		},
		{
			name:   "height only change resizes",
			startW: 20, startH: 4,
			cols: 20, rows: 8,
			wantW: 20, wantH: 8,
		},
		{
			name:   "no-op when dimensions already match",
			startW: 20, startH: 4,
			cols: 20, rows: 4,
			wantW: 20, wantH: 4,
			wantNoTouch: true,
		},
		{
			name:   "ignores zero cols",
			startW: 20, startH: 4,
			cols: 0, rows: 8,
			wantW: 20, wantH: 4,
			wantNoTouch: true,
		},
		{
			name:   "ignores zero rows",
			startW: 20, startH: 4,
			cols: 30, rows: 0,
			wantW: 20, wantH: 4,
			wantNoTouch: true,
		},
		{
			name:   "ignores negative cols",
			startW: 20, startH: 4,
			cols: -5, rows: 8,
			wantW: 20, wantH: 4,
			wantNoTouch: true,
		},
		{
			name:   "ignores negative rows",
			startW: 20, startH: 4,
			cols: 30, rows: -2,
			wantW: 20, wantH: 4,
			wantNoTouch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := vterm.New(tt.startW, tt.startH)
			term.Write([]byte("X"))
			// Capture the screen backing array's identity before the call. A real
			// resize reallocates term.Screen, so an unchanged header proves the
			// call short-circuited rather than rebuilding an identically-sized
			// buffer. This gives the no-op cases real discriminating power that a
			// content-only check (which a rebuild would preserve) cannot.
			before := screenHeader(term)

			ResizeTerminalForSessionRestore(term, tt.cols, tt.rows)

			if term.Width != tt.wantW || term.Height != tt.wantH {
				t.Fatalf("got %dx%d, want %dx%d", term.Width, term.Height, tt.wantW, tt.wantH)
			}
			after := screenHeader(term)
			if tt.wantNoTouch {
				if after != before {
					t.Fatalf("expected no-op resize to leave the screen buffer untouched, header changed %v -> %v", before, after)
				}
			} else if after == before {
				t.Fatalf("expected resize to rebuild the screen buffer, but the backing array was unchanged %v", before)
			}
		})
	}
}

// screenHeader returns the address of the screen slice's backing array, which
// changes whenever VTerm.Resize rebuilds the buffer and stays stable when the
// resize short-circuits as a no-op.
func screenHeader(term *vterm.VTerm) uintptr {
	return reflect.ValueOf(term.Screen).Pointer()
}

func TestResizeTerminalForSessionRestore_NilTerminalIsNoOp(t *testing.T) {
	// A nil terminal must be tolerated without panicking; the production call
	// sites guard reused tabs that may not have a live VTerm yet.
	ResizeTerminalForSessionRestore(nil, 80, 24)
}

func TestSessionSnapshotSize(t *testing.T) {
	tests := []struct {
		name            string
		captureFullPane bool
		snapCols        int
		snapRows        int
		fallbackCols    int
		fallbackRows    int
		wantCols        int
		wantRows        int
	}{
		{
			name:            "uses snapshot when full pane and positive",
			captureFullPane: true,
			snapCols:        80, snapRows: 24,
			fallbackCols: 40, fallbackRows: 10,
			wantCols: 80, wantRows: 24,
		},
		{
			name:            "falls back when not capturing full pane",
			captureFullPane: false,
			snapCols:        80, snapRows: 24,
			fallbackCols: 40, fallbackRows: 10,
			wantCols: 40, wantRows: 10,
		},
		{
			name:            "falls back when snapshot cols are zero",
			captureFullPane: true,
			snapCols:        0, snapRows: 24,
			fallbackCols: 40, fallbackRows: 10,
			wantCols: 40, wantRows: 10,
		},
		{
			name:            "falls back when snapshot rows are zero",
			captureFullPane: true,
			snapCols:        80, snapRows: 0,
			fallbackCols: 40, fallbackRows: 10,
			wantCols: 40, wantRows: 10,
		},
		{
			name:            "falls back when snapshot cols are negative",
			captureFullPane: true,
			snapCols:        -1, snapRows: 24,
			fallbackCols: 40, fallbackRows: 10,
			wantCols: 40, wantRows: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCols, gotRows := SessionSnapshotSize(
				tt.captureFullPane,
				tt.snapCols, tt.snapRows,
				tt.fallbackCols, tt.fallbackRows,
			)
			if gotCols != tt.wantCols || gotRows != tt.wantRows {
				t.Fatalf("got %dx%d, want %dx%d", gotCols, gotRows, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestSessionRestorePaneModeState_MapsEveryField(t *testing.T) {
	mode := tmux.PaneModeState{
		HasState:          true,
		AltScreen:         true,
		OriginMode:        true,
		CursorHidden:      true,
		ScrollTop:         3,
		ScrollBottom:      18,
		HasAltSavedCursor: true,
		AltSavedCursorX:   7,
		AltSavedCursorY:   9,
	}

	got := SessionRestorePaneModeState(mode)

	want := vterm.PaneModeState{
		HasState:              true,
		PreserveExistingState: false, // !HasState
		AltScreen:             true,
		OriginMode:            true,
		CursorHidden:          true,
		ScrollTop:             3,
		ScrollBottom:          18,
		HasAltSavedCursor:     true,
		AltSavedCursorX:       7,
		AltSavedCursorY:       9,
	}
	if got != want {
		t.Fatalf("mode mapping mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestSessionRestorePaneModeState_PreservesExistingStateWhenNoCapturedState(t *testing.T) {
	// When tmux reports no captured mode state, the mapped result must signal
	// the VTerm to keep whatever state it already holds.
	got := SessionRestorePaneModeState(tmux.PaneModeState{HasState: false})

	if got.HasState {
		t.Fatal("expected mapped state to report no captured state")
	}
	if !got.PreserveExistingState {
		t.Fatal("expected PreserveExistingState to be set when tmux has no captured state")
	}
}

func TestRestorePaneCapture_NilTerminalIsNoOp(t *testing.T) {
	// The early nil guard protects reused tabs whose VTerm is not yet live.
	RestorePaneCapture(
		nil,
		[]byte("history\n"),
		nil,
		0, 0, false,
		tmux.PaneModeState{},
		20, 2, 20, 2,
	)
}

func TestRestoreScrollbackCapture_NilTerminalIsNoOp(t *testing.T) {
	RestoreScrollbackCapture(nil, []byte("history\n"), 20, 1, 20, 2)
}

func TestRestoreScrollbackCapture_ResizesToLiveViewportAfterPrepend(t *testing.T) {
	term := vterm.New(20, 2)

	// Current viewport differs from the seeded terminal, so the restore must
	// resize the VTerm to the live PTY dimensions after prepending history.
	RestoreScrollbackCapture(term, []byte("ancient\n"), 20, 1, 30, 5)

	if term.Width != 30 || term.Height != 5 {
		t.Fatalf("expected restore to resize to live viewport, got %dx%d", term.Width, term.Height)
	}
}

func TestRestoreScrollbackCapture_SkipsResizeWhenViewportUnknown(t *testing.T) {
	term := vterm.New(20, 2)

	// Zero current dimensions mean the live viewport is unknown; the terminal
	// must keep its existing size rather than collapse to 0x0.
	RestoreScrollbackCapture(term, []byte("ancient\n"), 20, 1, 0, 0)

	if term.Width != 20 || term.Height != 2 {
		t.Fatalf("expected unknown viewport to leave dimensions unchanged, got %dx%d", term.Width, term.Height)
	}
}
