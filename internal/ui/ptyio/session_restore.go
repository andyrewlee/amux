package ptyio

import (
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

func SessionSnapshotSize(captureFullPane bool, snapshotCols, snapshotRows, fallbackCols, fallbackRows int) (int, int) {
	if captureFullPane && snapshotCols > 0 && snapshotRows > 0 {
		return snapshotCols, snapshotRows
	}
	return fallbackCols, fallbackRows
}

func SessionRestorePaneModeState(mode tmux.PaneModeState) vterm.PaneModeState {
	return vterm.PaneModeState{
		HasState:              mode.HasState,
		PreserveExistingState: !mode.HasState,
		AltScreen:             mode.AltScreen,
		OriginMode:            mode.OriginMode,
		CursorHidden:          mode.CursorHidden,
		ScrollTop:             mode.ScrollTop,
		ScrollBottom:          mode.ScrollBottom,
		HasAltSavedCursor:     mode.HasAltSavedCursor,
		AltSavedCursorX:       mode.AltSavedCursorX,
		AltSavedCursorY:       mode.AltSavedCursorY,
	}
}

func RestorePaneCapture(
	term *vterm.VTerm,
	data []byte,
	postAttachScrollback []byte,
	cursorX, cursorY int,
	hasCursor bool,
	mode tmux.PaneModeState,
	snapshotCols, snapshotRows int,
	currentCols, currentRows int,
) {
	if term == nil {
		return
	}
	restoreCols, restoreRows := SessionSnapshotSize(true, snapshotCols, snapshotRows, currentCols, currentRows)
	visibleHistoryRows := 0
	if term.Width != restoreCols || term.Height != restoreRows {
		term.Resize(restoreCols, restoreRows)
	}
	term.LoadSnapshot(vterm.TerminalSnapshot{
		Data:      data,
		Cols:      restoreCols,
		Rows:      restoreRows,
		CursorX:   cursorX,
		CursorY:   cursorY,
		HasCursor: hasCursor,
		ModeState: SessionRestorePaneModeState(mode),
	})
	if restoreCols != currentCols || restoreRows != currentRows {
		prevScrollbackLen := len(term.Scrollback)
		if mode.AltScreen && currentRows > restoreRows {
			term.ResizeWithoutHistoryReveal(currentCols, currentRows)
		} else {
			term.Resize(currentCols, currentRows)
			if len(term.Scrollback) < prevScrollbackLen {
				visibleHistoryRows = prevScrollbackLen - len(term.Scrollback)
			}
		}
	}
	term.AppendDelta(vterm.TerminalSnapshot{
		Data: postAttachScrollback,
		Cols: restoreCols,
		Rows: restoreRows,
	}, visibleHistoryRows)
}

func RestoreScrollbackCapture(
	term *vterm.VTerm,
	data []byte,
	captureCols, captureRows int,
	currentCols, currentRows int,
) {
	if term == nil {
		return
	}
	term.ScrollViewToBottom()
	// Restore lands on the live view; release any sync viewport anchor so a
	// sync that ends right after restore does not re-scroll into history.
	term.NoteSyncViewportInteraction()
	term.PrependHistory(vterm.TerminalSnapshot{Data: data, Cols: captureCols, Rows: captureRows})
	if currentCols > 0 && currentRows > 0 && (term.Width != currentCols || term.Height != currentRows) {
		term.Resize(currentCols, currentRows)
	}
}

// ResizeTerminalForSessionRestore ensures a reused local VTerm matches the PTY
// dimensions before replaying any tmux capture into it.
func ResizeTerminalForSessionRestore(term *vterm.VTerm, cols, rows int) {
	if term == nil || cols <= 0 || rows <= 0 {
		return
	}
	if term.Width == cols && term.Height == rows {
		return
	}
	term.Resize(cols, rows)
}

// TerminalSnapshotFromPane converts a tmux pane snapshot into the vterm
// snapshot type. tmux deliberately does not depend on vterm, so this boundary
// conversion lives here; the missing-mode-state policy is made explicit the
// same way SessionRestorePaneModeState does it (no tmux state → preserve the
// terminal's existing modes).
func TerminalSnapshotFromPane(snap tmux.PaneSnapshot) vterm.TerminalSnapshot {
	return vterm.TerminalSnapshot{
		Data:      snap.Data,
		Cols:      snap.Cols,
		Rows:      snap.Rows,
		CursorX:   snap.CursorX,
		CursorY:   snap.CursorY,
		HasCursor: snap.HasCursor,
		ModeState: SessionRestorePaneModeState(snap.ModeState),
	}
}
