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

// SessionRestoreCapture is the pane snapshot captured at (re)attach time and
// replayed into a fresh vterm by RestorePaneCapture.
//
// Invariant: a new snapshot field is a one-line addition here plus the
// producer struct literals — the message structs embed this type, so the
// field is promoted everywhere without touching consumer signatures.
type SessionRestoreCapture struct {
	ScrollbackCapture           []byte
	PostAttachScrollbackCapture []byte
	CaptureFullPane             bool
	SnapshotCols                int
	SnapshotRows                int
	SnapshotCursorX             int
	SnapshotCursorY             int
	SnapshotHasCursor           bool
	SnapshotModeState           tmux.PaneModeState
}

func RestorePaneCapture(term *vterm.VTerm, c SessionRestoreCapture, currentCols, currentRows int) {
	if term == nil {
		return
	}
	restoreCols, restoreRows := SessionSnapshotSize(true, c.SnapshotCols, c.SnapshotRows, currentCols, currentRows)
	visibleHistoryRows := 0
	if term.Width != restoreCols || term.Height != restoreRows {
		term.Resize(restoreCols, restoreRows)
	}
	term.LoadSnapshot(vterm.TerminalSnapshot{
		Data:      c.ScrollbackCapture,
		Cols:      restoreCols,
		Rows:      restoreRows,
		CursorX:   c.SnapshotCursorX,
		CursorY:   c.SnapshotCursorY,
		HasCursor: c.SnapshotHasCursor,
		ModeState: SessionRestorePaneModeState(c.SnapshotModeState),
	})
	if restoreCols != currentCols || restoreRows != currentRows {
		prevScrollbackLen := len(term.Scrollback)
		if c.SnapshotModeState.AltScreen && currentRows > restoreRows {
			term.ResizeWithoutHistoryReveal(currentCols, currentRows)
		} else {
			term.Resize(currentCols, currentRows)
			if len(term.Scrollback) < prevScrollbackLen {
				visibleHistoryRows = prevScrollbackLen - len(term.Scrollback)
			}
		}
	}
	term.AppendDelta(vterm.TerminalSnapshot{
		Data: c.PostAttachScrollbackCapture,
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
