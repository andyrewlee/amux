package vterm

// TerminalSnapshot is a captured pane image plus the metadata needed to
// replay it into a VTerm: the raw capture bytes, the geometry the capture was
// taken at, the cursor, and the pane mode state.
//
// It is the unified entry point for the capture-load paths. The historical
// per-shape variants (PrependScrollbackWithSize, LoadPaneCaptureWithCursorAndModes,
// AppendScrollbackDeltaWithSize) remain as the implementation, but new callers
// should construct a TerminalSnapshot and use LoadSnapshot / AppendDelta /
// PrependHistory.
//
// Missing mode state is an explicit decision, never a silent default: when
// ModeState.HasState is false, ModeState.PreserveExistingState selects
// between keeping the terminal's current modes (partial captures) and
// resetting them to defaults (authoritative captures). Builders of a
// TerminalSnapshot must set it deliberately.
type TerminalSnapshot struct {
	// Data is the raw ANSI capture (e.g. tmux capture-pane output).
	Data []byte
	// Cols/Rows are the dimensions the capture was taken at; zero values fall
	// back to the terminal's current size.
	Cols, Rows int
	// CursorX/CursorY position the cursor after a full load when HasCursor is
	// set.
	CursorX, CursorY int
	HasCursor        bool
	// ModeState carries alt-screen/origin/cursor-visibility/scroll-region
	// state, with the explicit missing-state policy described above.
	ModeState PaneModeState
}

// LoadSnapshot replaces the terminal's screen and scrollback with the
// snapshot content, applying cursor and mode state.
func (v *VTerm) LoadSnapshot(snap TerminalSnapshot) {
	v.LoadPaneCaptureWithCursorAndModes(snap.Data, snap.CursorX, snap.CursorY, snap.HasCursor, snap.ModeState)
}

// AppendDelta appends the snapshot rows that are missing from the current
// buffers (post-attach growth). visibleHistoryRows is the number of history
// rows that a preceding resize folded back into the visible screen, so they
// are not appended twice.
func (v *VTerm) AppendDelta(snap TerminalSnapshot, visibleHistoryRows int) {
	v.AppendScrollbackDeltaWithSize(snap.Data, snap.Cols, snap.Rows, visibleHistoryRows)
}

// PrependHistory inserts the snapshot rows above the existing scrollback
// (older captured history fetched after the live view was already populated).
func (v *VTerm) PrependHistory(snap TerminalSnapshot) {
	v.PrependScrollbackWithSize(snap.Data, snap.Cols, snap.Rows)
}
