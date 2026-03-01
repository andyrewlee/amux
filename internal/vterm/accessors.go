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

// CursorHiddenForRender returns the effective cursor-hidden state for rendering.
func (v *VTerm) CursorHiddenForRender() bool {
	return v.CursorHidden
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
// This increments on any visible mutation (content or cursor state).
func (v *VTerm) Version() uint64 {
	return v.version
}

// ContentVersion returns the content-only version counter.
// This increments on visible screen/view/selection mutations.
func (v *VTerm) ContentVersion() uint64 {
	return v.contentVersion
}

// CursorVersion returns the cursor-only version counter.
// This increments on cursor position/visibility mutations.
func (v *VTerm) CursorVersion() uint64 {
	return v.cursorVersion
}

// bumpVersion increments the version counter.
// Called internally when content changes.
func (v *VTerm) bumpVersion() {
	v.version++
	v.contentVersion++
}

// bumpCursorVersion increments version counters for cursor-only changes.
func (v *VTerm) bumpCursorVersion() {
	v.version++
	v.cursorVersion++
}

// bumpVersionIfCursorMoved bumps version if cursor position changed.
func (v *VTerm) bumpVersionIfCursorMoved(prevX, prevY int) {
	if v.CursorX != prevX || v.CursorY != prevY {
		v.bumpCursorVersion()
	}
}
