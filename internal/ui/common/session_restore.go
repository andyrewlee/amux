package common

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
	term.LoadPaneCaptureWithCursorAndModes(data, cursorX, cursorY, hasCursor, SessionRestorePaneModeState(mode))
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
	term.AppendScrollbackDeltaWithSize(postAttachScrollback, restoreCols, restoreRows, visibleHistoryRows)
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
	term.PrependScrollbackWithSize(data, captureCols, captureRows)
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
