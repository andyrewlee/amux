package vterm

// MouseReportingEnabled reports whether the hosted terminal application has
// requested mouse event reporting.
func (v *VTerm) MouseReportingEnabled() bool {
	return v != nil && v.mouseTrackingMode != 0
}

// MouseSGRMode reports whether SGR extended mouse coordinates are enabled.
func (v *VTerm) MouseSGRMode() bool {
	return v != nil && v.mouseSGRMode
}

// CursorRenderState is the cached cursor state from the previous render frame,
// used to detect cursor-only changes and mark the affected lines dirty.
type CursorRenderState struct {
	X, Y       int
	ShowCursor bool
	Hidden     bool
}

// LastCursorState returns the cursor position and visibility from the previous
// render frame.
func (v *VTerm) LastCursorState() CursorRenderState {
	return CursorRenderState{
		X:          v.lastCursorX,
		Y:          v.lastCursorY,
		ShowCursor: v.lastShowCursor,
		Hidden:     v.lastCursorHidden,
	}
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
// This increments whenever visible content changes.
func (v *VTerm) Version() uint64 {
	v.maybeReleaseStaleSync()
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
