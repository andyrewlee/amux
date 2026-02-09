package vterm

// LastCursorX returns the cursor X position from the previous render frame.
// Used to detect cursor movement and mark dirty lines.
func (v *VTerm) LastCursorX() int {
	return v.lastCursorX
}

// LastCursorY returns the cursor Y position from the previous render frame.
// Used to detect cursor movement and mark old cursor line dirty.
func (v *VTerm) LastCursorY() int {
	return v.lastCursorY
}

// LastShowCursor returns the cursor visibility from the previous render frame.
func (v *VTerm) LastShowCursor() bool {
	return v.lastShowCursor
}

// LastCursorHidden returns the last effective cursor hidden state used by rendering.
// Non-alt-screen sessions intentionally ignore DECTCEM cursor hide/show toggles to
// avoid flicker churn from chat-style CLIs that frequently emit ?25l/?25h.
func (v *VTerm) LastCursorHidden() bool {
	return v.lastCursorHidden
}

// CursorHiddenForRender returns whether the cursor should currently be hidden.
// We only honor DECTCEM cursor hide/show while in alt-screen mode.
func (v *VTerm) CursorHiddenForRender() bool {
	return v.CursorHidden && v.AltScreen
}

// SelActive reports whether a selection is active.
func (v *VTerm) SelActive() bool {
	return v.selActive
}

// SelStartX returns the selection start X.
func (v *VTerm) SelStartX() int {
	return v.selStartX
}

// SelStartLine returns the selection start line (absolute line number).
func (v *VTerm) SelStartLine() int {
	return v.selStartLine
}

// SelStartY returns the selection start Y in screen coordinates.
// Returns -1 if the start line is not visible.
func (v *VTerm) SelStartY() int {
	return v.AbsoluteLineToScreenY(v.selStartLine)
}

// SelEndX returns the selection end X.
func (v *VTerm) SelEndX() int {
	return v.selEndX
}

// SelEndLine returns the selection end line (absolute line number).
func (v *VTerm) SelEndLine() int {
	return v.selEndLine
}

// SelEndY returns the selection end Y in screen coordinates.
// Returns -1 if the end line is not visible.
func (v *VTerm) SelEndY() int {
	return v.AbsoluteLineToScreenY(v.selEndLine)
}

// Version returns the current version counter.
// This increments whenever visible content changes.
func (v *VTerm) Version() uint64 {
	return v.version
}

// bumpVersion increments the version counter.
// Called internally when content changes.
func (v *VTerm) bumpVersion() {
	v.version++
}

// bumpVersionIfCursorMoved bumps version if cursor position changed.
func (v *VTerm) bumpVersionIfCursorMoved(prevX, prevY int) {
	if v.CursorX != prevX || v.CursorY != prevY {
		v.bumpVersion()
	}
}
