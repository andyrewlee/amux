package vterm

// PaneModeState describes the VT mode state that should accompany an
// authoritative tmux pane snapshot.
type PaneModeState struct {
	HasState bool
	// PreserveExistingState keeps the current terminal mode state when a pane
	// snapshot did not include authoritative tmux mode fields.
	PreserveExistingState bool
	AltScreen             bool
	OriginMode            bool
	CursorHidden          bool
	ScrollTop             int
	ScrollBottom          int
	HasAltSavedCursor     bool
	AltSavedCursorX       int
	AltSavedCursorY       int
}

// ScreenYToAbsoluteLine converts a screen Y coordinate (0 to Height-1) to an absolute line number.
// Absolute line 0 is the first line in scrollback.
func (v *VTerm) ScreenYToAbsoluteLine(screenY int) int {
	// Total lines = scrollback + screen (respect sync snapshot if active)
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	totalLines := scrollbackLen + screenLen

	// The visible window starts at this absolute line
	startLine := totalLines - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	return startLine + screenY
}

// AbsoluteLineToScreenY converts an absolute line number to a screen Y coordinate.
// Returns -1 if the line is not currently visible.
func (v *VTerm) AbsoluteLineToScreenY(absLine int) int {
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	totalLines := scrollbackLen + screenLen

	// The visible window starts at this absolute line
	startLine := totalLines - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	screenY := absLine - startLine
	if screenY < 0 || screenY >= v.Height {
		return -1
	}
	return screenY
}

func (v *VTerm) currentMaxViewOffset() int {
	if v == nil {
		return 0
	}
	_, scrollbackLen := v.RenderBuffers()
	return scrollbackLen
}

// MaxViewOffset returns the maximum scrollback offset for the current buffers.
// Used by the sidebar/center wheel handlers to decide whether scrollback exists.
func (v *VTerm) MaxViewOffset() int {
	if v == nil {
		return 0
	}
	_, scrollbackLen := v.RenderBuffers()
	return scrollbackLen
}

func (v *VTerm) clampViewOffsetToCurrentMax() {
	if v == nil {
		return
	}
	maxOffset := v.currentMaxViewOffset()
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
}

// NoteSyncViewportInteraction records a viewport interaction during
// synchronized output. Scrolled into history, the frozen viewport stays
// anchored when the sync ends; back at the live view, the anchor is released
// and recorded hidden growth is discarded. This is an explicit API called by
// the UI scroll paths (mousewheel/keys) rather than a hidden side effect of
// the viewport math: programmatic ViewOffset changes do not touch the anchor
// unless their call site opts in.
func (v *VTerm) NoteSyncViewportInteraction() {
	if v == nil || !v.syncActive {
		return
	}
	v.syncPreserveViewport = v.ViewOffset > 0
}

func (v *VTerm) adjustAnchoredViewOffset(delta int) {
	if v == nil || delta == 0 {
		return
	}
	if v.syncActive {
		v.syncViewOffsetDelta += delta
		return
	}
	if v.ViewOffset <= 0 {
		return
	}
	v.ViewOffset += delta
	v.clampViewOffsetToCurrentMax()
}

func (v *VTerm) anchorViewOffsetForAddedLines(added int) {
	if v == nil || added <= 0 {
		return
	}
	v.adjustAnchoredViewOffset(added)
}

// ScrollView scrolls the view by delta lines (positive = up into history)
func (v *VTerm) ScrollView(delta int) {
	oldOffset := v.ViewOffset
	v.ViewOffset += delta
	v.clampViewOffsetToCurrentMax()
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewTo sets absolute scroll position
func (v *VTerm) ScrollViewTo(offset int) {
	oldOffset := v.ViewOffset
	v.ViewOffset = offset
	v.clampViewOffsetToCurrentMax()
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToTop scrolls to oldest content
func (v *VTerm) ScrollViewToTop() {
	oldOffset := v.ViewOffset
	v.ViewOffset = v.currentMaxViewOffset()
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToBottom returns to live view
func (v *VTerm) ScrollViewToBottom() {
	oldOffset := v.ViewOffset
	v.ViewOffset = 0
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// IsScrolled returns true if viewing scrollback
func (v *VTerm) IsScrolled() bool {
	return v.ViewOffset > 0
}

// GetScrollInfo returns (current offset, max offset)
func (v *VTerm) GetScrollInfo() (int, int) {
	return v.ViewOffset, v.currentMaxViewOffset()
}

// PrependScrollback parses captured scrollback content (with ANSI escapes) and
// prepends the resulting lines to the scrollback buffer. This is used to
// populate scrollback history when attaching to an existing tmux session.
// It is a no-op if data is empty.
func (v *VTerm) PrependScrollback(data []byte) {
	v.PrependScrollbackWithSize(data, v.Width, v.Height)
}

// PrependScrollbackWithSize parses captured scrollback content using the cell
// geometry that tmux had when it produced the capture, then prepends the
// resulting lines to the scrollback buffer.
func (v *VTerm) PrependScrollbackWithSize(data []byte, width, height int) {
	if len(data) == 0 {
		return
	}
	if width <= 0 {
		width = v.Width
	}
	if height <= 0 {
		height = v.Height
	}

	// tmux capture-pane output is newline-delimited rows, not a raw PTY stream,
	// so each LF must reset to column 0 while we parse the snapshot.
	tmp := parseCaptureWithSize(data, width, height)
	if tmp == nil {
		return
	}

	lines := captureLines(data, tmp)
	if len(lines) == 0 {
		return
	}

	// Prepend captured lines before existing scrollback.
	newScrollback := make([][]Cell, 0, len(lines)+len(v.Scrollback))
	newScrollback = appendTrimmedRows(newScrollback, lines)
	newScrollback = append(newScrollback, v.Scrollback...)
	v.Scrollback = newScrollback
	v.trimScrollback()
}

// AppendScrollbackDelta appends only the missing suffix from a newer tmux
// history capture. This is used after a pre-attach full-pane snapshot so rows
// that scrolled into history during the snapshot->attach gap are preserved
// without replacing the restored visible frame.
func (v *VTerm) AppendScrollbackDelta(data []byte) {
	if len(data) == 0 {
		return
	}

	tmp := v.parseCapture(data)
	if tmp == nil {
		return
	}

	lines := captureLines(data, tmp)
	if len(lines) == 0 {
		return
	}

	matchStart := appendScrollbackDeltaMatchStart(lines, v.Scrollback, v.Screen)
	if matchStart < 0 {
		return
	}
	matchEnd := matchStart + len(v.Scrollback)
	if matchEnd == len(lines) {
		return
	}

	added := 0
	for _, line := range lines[matchEnd:] {
		v.Scrollback = append(v.Scrollback, copyLineTrimmed(line))
		added++
	}
	if added > 0 {
		v.invalidateTrackedAltScreenCapture()
		v.anchorViewOffsetForAddedLines(added)
	}
	v.trimScrollback()
}

// AppendScrollbackDeltaWithSize appends the missing suffix from a newer tmux
// history capture parsed at the tmux geometry that produced it. When the local
// viewport has changed since capture time, visibleHistoryRows indicates how
// many of the newest captured rows are already visible after the restore/resize
// and should not be appended back into scrollback.
func (v *VTerm) AppendScrollbackDeltaWithSize(data []byte, width, height, visibleHistoryRows int) {
	if len(data) == 0 {
		return
	}
	if width <= 0 {
		width = v.Width
	}
	if height <= 0 {
		height = v.Height
	}
	if visibleHistoryRows < 0 {
		visibleHistoryRows = 0
	}

	tmp := parseCaptureWithSize(data, width, height)
	if tmp == nil {
		return
	}

	lines := captureLines(data, tmp)
	if len(lines) == 0 {
		return
	}
	if (width != v.Width || height != v.Height) && visibleHistoryRows >= 0 {
		visibleTailRows := appendScrollbackDeltaVisibleTailOnScreen(lines, v.Screen)
		if visibleTailRows > visibleHistoryRows {
			visibleHistoryRows = visibleTailRows
		}
	}
	if visibleHistoryRows > len(lines) {
		visibleHistoryRows = len(lines)
	}
	lines = lines[:len(lines)-visibleHistoryRows]
	if len(lines) == 0 {
		return
	}

	matchStart := appendScrollbackDeltaMatchStart(lines, v.Scrollback, v.Screen)
	if matchStart < 0 {
		return
	}
	matchEnd := matchStart + len(v.Scrollback)
	if matchEnd == len(lines) {
		return
	}

	added := 0
	for _, line := range lines[matchEnd:] {
		v.Scrollback = append(v.Scrollback, copyLineTrimmed(line))
		added++
	}
	if added > 0 {
		v.invalidateTrackedAltScreenCapture()
		v.anchorViewOffsetForAddedLines(added)
	}
	v.trimScrollback()
}
