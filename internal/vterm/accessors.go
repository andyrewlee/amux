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

// AlternateScrollMode reports whether the hosted application requested DECSET
// 1007. It is tracked for parser fidelity only: routing intentionally does
// not key on it, because tmux absorbs the pane's 1007 and never relays it to
// the client — the wheel contract keys on the probed pane screen instead
// (see WheelScrollBecomesCursorKeys).
func (v *VTerm) AlternateScrollMode() bool {
	return v != nil && v.altScrollMode
}

// WheelScrollBecomesCursorKeys reports whether a wheel event addressed to this
// pane must be encoded as arrow-key presses rather than mouse sequences: the
// pane's application runs in the alternate screen (per the authoritative tmux
// probe) and did not claim the mouse. This is the behavior tmux itself gives
// fullscreen panes under alternate scroll — a pane's DECSET 1007 request is
// absorbed by tmux and never reaches the client, so the conversion keys on
// the pane's probed screen, not on relayed mode bits. Mouse reporting, when
// the application asks for it, always wins.
func (v *VTerm) WheelScrollBecomesCursorKeys() bool {
	return v != nil && v.PaneAlternateScreen && !v.MouseReportingEnabled()
}

// ApplicationCursorKeys reports whether DECCKM is set, so arrow keys (and
// wheel events converted to them) are encoded as SS3 rather than CSI.
func (v *VTerm) ApplicationCursorKeys() bool {
	return v != nil && v.applicationCursorKeys
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

// PrivateModesSeen reports whether the application has issued at least one
// private-mode (DECSET/DECRST) sequence — the cheapest "input loop is live"
// signal available: TUI agents enable mouse reporting, alt screen, or
// synchronized output only after their event loop starts reading stdin.
func (v *VTerm) PrivateModesSeen() bool {
	return v.privateModesSeen > 0
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
