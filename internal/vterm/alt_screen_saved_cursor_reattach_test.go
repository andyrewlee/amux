package vterm

import "testing"

// The per-buffer DECSC slot has to survive tmux reattach, which changes the
// alt-screen flag out of band rather than through the mode sequences. These
// tests cover the two orders that can leave the stash unpaired — the regression
// risk that kept this fix deferred.

// TestReattachOntoPrimaryDoesNotResurrectStaleSavedCursor asserts a reattach
// that lands on the primary screen wins over a stash left behind by a
// pre-detach alt-screen entry. Restoring the pre-detach position over the
// freshly restored one would move the cursor to a place the pane is no longer
// showing.
func TestReattachOntoPrimaryDoesNotResurrectStaleSavedCursor(t *testing.T) {
	vt := New(20, 5)

	// Before detaching: save a cursor on the primary screen, then enter alt.
	vt.Write([]byte("\x1b[3;5H"))
	vt.Write([]byte("\x1b7"))
	vt.Write([]byte("\x1b[?1049h"))
	if !vt.inAltSavedCursor {
		t.Fatal("setup: entering the alt screen did not stash the primary saved cursor")
	}

	// Reattach reports the pane is on the primary screen.
	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("restored line\n"),
		2,
		0,
		true,
		PaneModeState{HasState: true, AltScreen: false, ScrollTop: 0, ScrollBottom: 5},
	)

	if vt.AltScreen {
		t.Fatal("reattach reported the primary screen but the terminal stayed on alt")
	}
	if vt.inAltSavedCursor {
		t.Fatal("the stale stash survived a reattach onto the primary screen")
	}

	// The restore reseeded the DECSC slot from the restored cursor; DECRC must
	// use that, not the pre-detach position.
	restored := [2]int{vt.SavedCursorX, vt.SavedCursorY}
	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("\x1b8"))
	if got := cursorAt(vt); got != restored {
		t.Fatalf("DECRC after reattach went to %v, want the reseeded %v", got, restored)
	}
}

// TestReattachOntoAltThenExitKeepsRestoredSavedCursor asserts the other order:
// a reattach that lands on the alt screen without a matching entry must not let
// the eventual 1049l install a zeroed saved cursor over the restored one.
func TestReattachOntoAltThenExitKeepsRestoredSavedCursor(t *testing.T) {
	vt := New(20, 5)
	vt.Write([]byte("shell output"))

	vt.LoadPaneCaptureWithCursorAndModes(
		[]byte("app frame\n"),
		3,
		0,
		true,
		PaneModeState{
			HasState:          true,
			AltScreen:         true,
			ScrollTop:         0,
			ScrollBottom:      5,
			HasAltSavedCursor: true,
			AltSavedCursorX:   2,
			AltSavedCursorY:   1,
		},
	)
	if !vt.AltScreen {
		t.Fatal("setup: reattach did not land on the alt screen")
	}
	restored := [2]int{vt.SavedCursorX, vt.SavedCursorY}

	// The application exits normally after the reattach: no matching entry ever
	// ran in this terminal, so there is no stash to restore.
	vt.Write([]byte("\x1b[?1049l"))
	if vt.AltScreen {
		t.Fatal("1049l did not exit the alt screen")
	}
	if got := [2]int{vt.SavedCursorX, vt.SavedCursorY}; got != restored {
		t.Fatalf("an unpaired exit overwrote the restored saved cursor: got %v, want %v", got, restored)
	}
	if vt.CursorX != 2 || vt.CursorY != 1 {
		t.Fatalf("1049l restored the cursor to (%d,%d), want the reattached alt cursor (2,1)",
			vt.CursorX, vt.CursorY)
	}
}

// TestDetachlessAltCycleStillSwapsPerBuffer is the control for both tests
// above: with no reattach in the picture, an ordinary enter/exit cycle still
// swaps the DECSC slot per buffer.
func TestDetachlessAltCycleStillSwapsPerBuffer(t *testing.T) {
	vt := New(20, 5)

	vt.Write([]byte("\x1b[3;5H"))
	vt.Write([]byte("\x1b7"))
	want := [2]int{vt.SavedCursorX, vt.SavedCursorY}

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("\x1b[2;2H"))
	vt.Write([]byte("\x1b7"))
	vt.Write([]byte("\x1b[?1049l"))

	if got := [2]int{vt.SavedCursorX, vt.SavedCursorY}; got != want {
		t.Fatalf("saved cursor after a clean alt cycle = %v, want %v", got, want)
	}
	if vt.inAltSavedCursor {
		t.Fatal("the stash was not cleared by a balanced exit")
	}
}
