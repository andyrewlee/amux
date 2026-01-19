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

// LastCursorHidden returns the DECTCEM cursor hidden state from the previous render frame.
func (v *VTerm) LastCursorHidden() bool {
	return v.lastCursorHidden
}

// SelActive reports whether a selection is active.
func (v *VTerm) SelActive() bool {
	return v.selActive
}

// SelStartX returns the selection start X.
func (v *VTerm) SelStartX() int {
	return v.selStartX
}

// SelStartY returns the selection start Y.
func (v *VTerm) SelStartY() int {
	return v.selStartY
}

// SelEndX returns the selection end X.
func (v *VTerm) SelEndX() int {
	return v.selEndX
}

// SelEndY returns the selection end Y.
func (v *VTerm) SelEndY() int {
	return v.selEndY
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
