package vterm

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
	if len(data) == 0 {
		return
	}

	// Use a temporary vterm to parse the ANSI content into styled cells.
	tmp := New(v.Width, v.Height)
	tmp.Write(data)

	// Collect lines: scrollback first, then screen lines (trim trailing unused rows).
	var lines [][]Cell
	for _, line := range tmp.Scrollback {
		lines = append(lines, CopyLine(line))
	}
	screenLines := make([][]Cell, 0, len(tmp.Screen))
	for _, line := range tmp.Screen {
		screenLines = append(screenLines, CopyLine(line))
	}
	lastNonBlank := len(screenLines) - 1
	for lastNonBlank >= 0 && isBlankLine(screenLines[lastNonBlank]) {
		lastNonBlank--
	}
	if lastNonBlank >= 0 {
		lines = append(lines, screenLines[:lastNonBlank+1]...)
	}

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

// isBlankLine returns true if every cell in the line is the default blank cell.
func isBlankLine(line []Cell) bool {
	for _, c := range line {
		if c.Rune != ' ' && c.Rune != 0 {
			return false
		}
	}
	return true
}
