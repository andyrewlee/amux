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

// ScrollView scrolls the view by delta lines (positive = up into history)
func (v *VTerm) ScrollView(delta int) {
	oldOffset := v.ViewOffset
	v.ViewOffset += delta
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewTo sets absolute scroll position
func (v *VTerm) ScrollViewTo(offset int) {
	oldOffset := v.ViewOffset
	v.ViewOffset = offset
	maxOffset := len(v.Scrollback)
	if v.ViewOffset > maxOffset {
		v.ViewOffset = maxOffset
	}
	if v.ViewOffset < 0 {
		v.ViewOffset = 0
	}
	if v.ViewOffset != oldOffset {
		v.bumpVersion()
	}
}

// ScrollViewToTop scrolls to oldest content
func (v *VTerm) ScrollViewToTop() {
	oldOffset := v.ViewOffset
	v.ViewOffset = len(v.Scrollback)
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
	return v.ViewOffset, len(v.Scrollback)
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
	newScrollback = append(newScrollback, lines...)
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
		v.Scrollback = append(v.Scrollback, CopyLine(line))
		added++
	}
	if added > 0 {
		v.invalidateTrackedAltScreenCapture()
		if v.ViewOffset > 0 {
			v.ViewOffset += added
			if v.ViewOffset > len(v.Scrollback) {
				v.ViewOffset = len(v.Scrollback)
			}
		}
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
		v.Scrollback = append(v.Scrollback, CopyLine(line))
		added++
	}
	if added > 0 {
		v.invalidateTrackedAltScreenCapture()
		if v.ViewOffset > 0 {
			v.ViewOffset += added
			if v.ViewOffset > len(v.Scrollback) {
				v.ViewOffset = len(v.Scrollback)
			}
		}
	}
	v.trimScrollback()
}

// LoadPaneCapture replaces the terminal screen + scrollback with a full tmux
// pane capture (scrollback plus the current visible screen). This seeds a
// reattached client with the latest frame before live PTY output resumes.
func (v *VTerm) LoadPaneCapture(data []byte) {
	v.LoadPaneCaptureWithCursorAndModes(data, 0, 0, false, PaneModeState{PreserveExistingState: true})
}

// LoadPaneCaptureWithCursor replaces the terminal screen + scrollback with a
// full tmux pane capture and applies a separately captured tmux cursor position
// when one is available.
func (v *VTerm) LoadPaneCaptureWithCursor(data []byte, cursorX, cursorY int, hasCursor bool) {
	v.LoadPaneCaptureWithCursorAndModes(data, cursorX, cursorY, hasCursor, PaneModeState{PreserveExistingState: true})
}

// LoadPaneCaptureWithCursorAndModes replaces the terminal screen + scrollback
// with a full tmux pane capture and applies the accompanying tmux VT mode state
// when one is available.
func (v *VTerm) LoadPaneCaptureWithCursorAndModes(
	data []byte,
	cursorX, cursorY int,
	hasCursor bool,
	modeState PaneModeState,
) {
	v.loadPaneCaptureWithCursor(data, cursorX, cursorY, hasCursor, modeState)
}

func (v *VTerm) loadPaneCaptureWithCursor(
	data []byte,
	cursorX, cursorY int,
	hasCursor bool,
	modeState PaneModeState,
) {
	var tmp *VTerm
	if len(data) > 0 {
		tmp = v.parseCapture(data)
		if tmp == nil {
			return
		}
	}

	// A tmux pane snapshot is a complete frame. If the terminal was detached in
	// the middle of DEC synchronized output, the frozen sync buffers must be
	// cleared before publishing the restored frame.
	v.setSynchronizedOutput(false)
	if v.parser != nil {
		v.parser.Reset()
	}
	v.applyPaneModeState(modeState)
	v.ClearSelection()
	v.ViewOffset = 0
	v.Scrollback = v.Scrollback[:0]
	if tmp != nil {
		for _, line := range tmp.Scrollback {
			v.Scrollback = append(v.Scrollback, CopyLine(line))
		}
	}
	v.trimScrollback()

	newScreen := make([][]Cell, v.Height)
	for i := 0; i < v.Height; i++ {
		if tmp != nil && i < len(tmp.Screen) {
			newScreen[i] = CopyLine(tmp.Screen[i])
			continue
		}
		newScreen[i] = MakeBlankLine(v.Width)
	}
	v.Screen = newScreen
	if tmp != nil {
		v.CurrentStyle = tmp.CurrentStyle
	} else {
		v.CurrentStyle = Style{}
	}
	if hasCursor {
		v.CursorX = cursorX
		v.CursorY = cursorY
	} else if tmp != nil {
		// Full-pane restore is authoritative; when tmux omits explicit cursor
		// metadata, use the cursor implied by the restored frame instead of
		// reusing stale coordinates from the detached terminal.
		v.CursorX = tmp.CursorX
		v.CursorY = tmp.CursorY
	} else {
		v.CursorX = 0
		v.CursorY = 0
	}
	v.clampCursor()
	v.SavedCursorX = v.CursorX
	v.SavedCursorY = v.CursorY
	v.SavedStyle = v.CurrentStyle
	if v.AltScreen {
		v.invalidateAltScreenCapture()
		v.trackRestoredAltScreenFrame()
	} else {
		v.invalidateAltScreenCapture()
	}
	v.invalidateRenderCache()
	v.ensureRenderCache(v.Height)
}

func (v *VTerm) applyPaneModeState(modeState PaneModeState) {
	previousScreen := copyScreenLines(v.Screen)
	previousAltScreenBuf := copyScreenLines(v.altScreenBuf)
	previousAltCursorX := v.altCursorX
	previousAltCursorY := v.altCursorY
	if !modeState.HasState {
		if modeState.PreserveExistingState {
			return
		}
		v.AltScreen = false
		v.altScreenBuf = nil
		v.altCursorX = 0
		v.altCursorY = 0
		v.ScrollTop = 0
		v.ScrollBottom = v.Height
		v.OriginMode = false
		v.CursorHidden = false
		return
	}
	if modeState.AltScreen {
		v.AltScreen = true
		switch {
		case previousAltScreenBuf != nil:
			v.altScreenBuf = previousAltScreenBuf
		case previousScreen != nil && !isBlankScreen(previousScreen):
			// tmux does not expose the hidden main-screen viewport while a pane
			// is actively in alt-screen mode, so only reuse local main-screen
			// state we already had. Fresh restores fall back to a blank buffer
			// rather than fabricating one from ordinary scrollback history.
			v.altScreenBuf = previousScreen
		default:
			v.altScreenBuf = v.makeScreen(v.Width, v.Height)
		}
		if modeState.HasAltSavedCursor {
			v.altCursorX = modeState.AltSavedCursorX
			v.altCursorY = modeState.AltSavedCursorY
		} else if previousAltScreenBuf != nil {
			v.altCursorX = previousAltCursorX
			v.altCursorY = previousAltCursorY
		} else {
			v.altCursorX = 0
			v.altCursorY = 0
		}
		v.clampAltSavedCursor()
	} else {
		v.AltScreen = false
		v.altScreenBuf = nil
		v.altCursorX = 0
		v.altCursorY = 0
	}
	scrollTop := modeState.ScrollTop
	scrollBottom := modeState.ScrollBottom
	if scrollTop < 0 || scrollTop >= v.Height || scrollBottom <= scrollTop || scrollBottom > v.Height {
		scrollTop = 0
		scrollBottom = v.Height
	}
	v.ScrollTop = scrollTop
	v.ScrollBottom = scrollBottom
	v.OriginMode = modeState.OriginMode
	v.CursorHidden = modeState.CursorHidden
}

func copyScreenLines(lines [][]Cell) [][]Cell {
	if lines == nil {
		return nil
	}
	copied := make([][]Cell, len(lines))
	for i, line := range lines {
		copied[i] = CopyLine(line)
	}
	return copied
}

func (v *VTerm) parseCapture(data []byte) *VTerm {
	if len(data) == 0 {
		return nil
	}
	return parseCaptureWithSize(data, v.Width, v.Height)
}

// isBlankLine returns true if every cell in the line is the default blank cell.
func isBlankLine(line []Cell) bool {
	for _, c := range line {
		if c.Rune != ' ' && c.Rune != 0 {
			return false
		}
	}
	return true
}
