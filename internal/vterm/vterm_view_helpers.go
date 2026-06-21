package vterm

// ScrollViewAndNote scrolls the view by delta lines and then records the
// resulting viewport interaction, exactly as if the caller had written
// ScrollView followed by NoteSyncViewportInteraction. UI scroll paths that
// move the viewport in response to user input (mousewheel, drag-select edge
// scroll, PgUp/PgDown) pair these two calls; this helper keeps that pairing
// in one place so the anchor bookkeeping cannot drift between call sites.
func (v *VTerm) ScrollViewAndNote(delta int) {
	v.ScrollView(delta)
	v.NoteSyncViewportInteraction()
}

// VTermHasScrollback reports whether v is non-nil and currently has scrollback
// the viewport can move into. It is the shared leaf used by the per-pane
// CanConsumeWheel checks to avoid hover-wheel focus steals from panes with no
// scrollable history.
func VTermHasScrollback(v *VTerm) bool {
	return v != nil && v.MaxViewOffset() > 0
}
