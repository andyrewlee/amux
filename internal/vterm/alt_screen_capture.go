package vterm

// captureScreenToScrollback copies the visible alt-screen frame into the
// scrollback buffer. It trims leading/trailing blank rows and clips each row
// to the current terminal width so only what was actually visible is stored.
// This is called before erase-display in alt-screen mode so that TUI content
// (e.g. Claude Code plan mode) is preserved for amux scroll-back. A dedup
// check avoids storing identical consecutive frames.
func (v *VTerm) captureScreenToScrollback() {
	lines := v.visibleCaptureFrame()
	if len(lines) == 0 {
		return
	}

	oldViewOffset := v.ViewOffset
	if v.matchesTrackedAltScreenCapture(lines) {
		return
	}
	removed, dropped := v.dropTrackedAltScreenCapture()

	deductOffset := func() {
		if oldViewOffset <= 0 {
			return
		}
		v.ViewOffset = oldViewOffset - removed
		if v.ViewOffset < 0 {
			v.ViewOffset = 0
		}
		if v.ViewOffset > len(v.Scrollback) {
			v.ViewOffset = len(v.Scrollback)
		}
	}

	// Dedup: skip if these lines match the tail of scrollback
	if matchesScrollbackTail(v.Scrollback, lines) {
		v.altScreenCaptureLen = len(lines)
		v.altScreenCaptureTracked = len(dropped) > 0 && captureRowsMatch(lines, dropped, v.Width)
		deductOffset()
		return
	}

	added := 0
	for _, line := range lines {
		v.Scrollback = append(v.Scrollback, CopyLine(line))
		added++
	}
	v.altScreenCaptureLen = added
	v.altScreenCaptureTracked = true
	if oldViewOffset > 0 {
		v.ViewOffset = oldViewOffset - removed + added
		if v.ViewOffset < 0 {
			v.ViewOffset = 0
		}
		if v.ViewOffset > len(v.Scrollback) {
			v.ViewOffset = len(v.Scrollback)
		}
	}
	v.trimScrollback()
}

func (v *VTerm) visibleCaptureFrame() [][]Cell {
	visible := make([][]Cell, len(v.Screen))
	firstNonBlank := -1
	lastNonBlank := -1

	for y, line := range v.Screen {
		visible[y] = copyVisibleLine(line, v.Width)
		if !isVisiblyBlankLine(visible[y]) {
			if firstNonBlank < 0 {
				firstNonBlank = y
			}
			lastNonBlank = y
		}
	}

	if firstNonBlank < 0 {
		return nil
	}
	return visible[firstNonBlank : lastNonBlank+1]
}

func copyVisibleLine(line []Cell, width int) []Cell {
	if width < 0 {
		width = 0
	}
	visible := MakeBlankLine(width)
	if width == 0 || len(line) == 0 {
		return visible
	}
	n := width
	if n > len(line) {
		n = len(line)
	}
	copy(visible, line[:n])
	normalizeLine(visible)
	return visible
}

func isVisiblyBlankLine(line []Cell) bool {
	var defaultStyle Style
	for _, c := range line {
		if c.Rune != ' ' && c.Rune != 0 {
			return false
		}
		if c.Style != defaultStyle {
			return false
		}
	}
	return true
}

func (v *VTerm) matchesTrackedAltScreenCapture(lines [][]Cell) bool {
	if v.altScreenCaptureLen <= 0 || !v.altScreenCaptureTracked || v.altScreenCaptureLen != len(lines) {
		return false
	}
	sb := len(v.Scrollback)
	if sb < v.altScreenCaptureLen {
		v.altScreenCaptureLen = 0
		v.altScreenCaptureTracked = false
		return false
	}
	return matchesScrollbackTail(v.Scrollback, lines)
}

func (v *VTerm) dropTrackedAltScreenCapture() (int, [][]Cell) {
	if v.altScreenCaptureLen <= 0 || !v.altScreenCaptureTracked {
		if v.altScreenCaptureLen <= 0 {
			return 0, nil
		}
		v.altScreenCaptureLen = 0
		return 0, nil
	}
	if len(v.Scrollback) < v.altScreenCaptureLen {
		v.altScreenCaptureLen = 0
		v.altScreenCaptureTracked = false
		return 0, nil
	}
	start := len(v.Scrollback) - v.altScreenCaptureLen
	// Copy the removed rows so the returned slice doesn't alias the
	// Scrollback backing array — a subsequent append could overwrite it.
	src := v.Scrollback[start:]
	removedRows := make([][]Cell, len(src))
	copy(removedRows, src)
	removed := v.altScreenCaptureLen
	v.Scrollback = v.Scrollback[:len(v.Scrollback)-removed]
	v.altScreenCaptureLen = 0
	v.altScreenCaptureTracked = false
	return removed, removedRows
}

func (v *VTerm) invalidateAltScreenCapture() {
	v.altScreenCaptureLen = 0
	v.altScreenCaptureTracked = false
}

// captureRowsMatch compares lines with captured rows using the current terminal width.
func captureRowsMatch(current, captured [][]Cell, width int) bool {
	if len(current) != len(captured) {
		return false
	}
	for i := range current {
		if !linesEqual(current[i], copyVisibleLine(captured[i], width)) {
			return false
		}
	}
	return true
}

// matchesScrollbackTail returns true if the last len(lines) entries in
// scrollback are cell-identical to lines.
func matchesScrollbackTail(scrollback, lines [][]Cell) bool {
	n := len(lines)
	sb := len(scrollback)
	if sb < n || n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if !linesEqual(scrollback[sb-n+i], lines[i]) {
			return false
		}
	}
	return true
}

// linesEqual returns true if two cell slices have identical runes and styles.
func linesEqual(a, b []Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Rune != b[i].Rune || a[i].Style != b[i].Style {
			return false
		}
	}
	return true
}
