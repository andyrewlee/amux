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
	if matched, dedupRemoved := v.matchesTrackedAltScreenCapture(lines); matched {
		if oldViewOffset > 0 {
			v.ViewOffset = oldViewOffset - dedupRemoved
			if v.ViewOffset < 0 {
				v.ViewOffset = 0
			}
			if v.ViewOffset > len(v.Scrollback) {
				v.ViewOffset = len(v.Scrollback)
			}
		}
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

	// Partial overlap detection — skip lines already in scrollback from scrollUp
	overlap := scrollbackTailOverlap(v.Scrollback, lines)

	added := 0
	for _, line := range lines[overlap:] {
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

// matchesTrackedAltScreenCapture checks if lines match the previously captured
// alt-screen content. The capture may no longer be at the scrollback tail if
// scrollUp has appended lines after it (tracked by altScreenCaptureEndOffset).
func (v *VTerm) matchesTrackedAltScreenCapture(lines [][]Cell) (bool, int) {
	if v.altScreenCaptureLen <= 0 || !v.altScreenCaptureTracked || v.altScreenCaptureLen != len(lines) {
		return false, 0
	}
	total := v.altScreenCaptureLen + v.altScreenCaptureEndOffset
	sb := len(v.Scrollback)
	if sb < total {
		v.altScreenCaptureLen = 0
		v.altScreenCaptureTracked = false
		v.altScreenCaptureEndOffset = 0
		return false, 0
	}
	captureStart := sb - total
	for i := 0; i < v.altScreenCaptureLen; i++ {
		if !linesEqual(v.Scrollback[captureStart+i], lines[i]) {
			return false, 0
		}
	}

	// Match confirmed — dedup scrollUp trailing lines that duplicate
	// content already present in the pre-capture scrollback.
	removed := v.dedupScrollUpTrailing(captureStart)
	return true, removed
}

// dropTrackedAltScreenCapture removes the previously captured alt-screen
// content from scrollback. With altScreenCaptureEndOffset, the capture may
// be in the middle of scrollback (not at the tail), so we remove from its
// tracked position and preserve trailing scrollUp lines.
func (v *VTerm) dropTrackedAltScreenCapture() (int, [][]Cell) {
	if v.altScreenCaptureLen <= 0 || !v.altScreenCaptureTracked {
		if v.altScreenCaptureLen <= 0 {
			v.altScreenCaptureEndOffset = 0
			return 0, nil
		}
		v.altScreenCaptureLen = 0
		v.altScreenCaptureEndOffset = 0
		return 0, nil
	}
	total := v.altScreenCaptureLen + v.altScreenCaptureEndOffset
	if len(v.Scrollback) < total {
		v.altScreenCaptureLen = 0
		v.altScreenCaptureTracked = false
		v.altScreenCaptureEndOffset = 0
		return 0, nil
	}
	captureStart := len(v.Scrollback) - total
	captureEnd := captureStart + v.altScreenCaptureLen

	// Copy the removed rows so the returned slice doesn't alias the
	// Scrollback backing array — a subsequent append could overwrite it.
	src := v.Scrollback[captureStart:captureEnd]
	removedRows := make([][]Cell, len(src))
	copy(removedRows, src)
	removed := v.altScreenCaptureLen

	// Remove capture from its position (preserving trailing scrollUp lines)
	v.Scrollback = append(v.Scrollback[:captureStart], v.Scrollback[captureEnd:]...)
	v.altScreenCaptureLen = 0
	v.altScreenCaptureTracked = false

	// Dedup scrollUp trailing lines against pre-capture scrollback.
	// After removal, trailing lines are at [captureStart, captureStart+endOffset).
	dedupRemoved := v.dedupScrollUpTrailing(captureStart)
	removed += dedupRemoved
	v.altScreenCaptureEndOffset = 0

	return removed, removedRows
}

// dedupScrollUpTrailing removes scrollUp lines from the scrollback that
// duplicate content already present in the pre-capture scrollback region
// (scrollback[:preCaptureLen]). This prevents duplication when TUI redraws
// cause the same content to scroll off multiple times across erase cycles.
//
// Known limitation: only compares trailing lines against pre-capture content.
// When above-fold content changes across cycles but below-fold stays the same,
// trailing lines accumulate without dedup (e.g. [X,Y] -> [X,Y,X,Y] -> ...).
// This is bounded by MaxScrollback and only affects edge cases where the top
// portion of a TUI changes while the bottom stays identical. Fixing would
// require tracking new-vs-old trailing lines to detect internal repetition.
func (v *VTerm) dedupScrollUpTrailing(preCaptureLen int) int {
	trailing := v.altScreenCaptureEndOffset
	if trailing <= 0 {
		v.altScreenCaptureEndOffset = 0
		return 0
	}

	if preCaptureLen <= 0 || preCaptureLen > len(v.Scrollback) {
		return 0
	}

	before := v.Scrollback[:preCaptureLen]
	trailingStart := len(v.Scrollback) - trailing
	if trailingStart < preCaptureLen {
		trailingStart = preCaptureLen
	}
	if trailingStart >= len(v.Scrollback) {
		return 0
	}

	trailingLines := v.Scrollback[trailingStart:]

	overlap := scrollbackTailOverlap(before, trailingLines)
	if overlap <= 0 {
		return 0
	}

	// Remove the overlapping prefix from the trailing lines
	v.Scrollback = append(v.Scrollback[:trailingStart], v.Scrollback[trailingStart+overlap:]...)
	v.altScreenCaptureEndOffset = trailing - overlap
	return overlap
}

func (v *VTerm) invalidateAltScreenCapture() {
	v.altScreenCaptureLen = 0
	v.altScreenCaptureTracked = false
	v.altScreenCaptureEndOffset = 0
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

// scrollbackTailOverlap returns the length of the longest suffix of scrollback
// that matches a prefix of lines. This detects lines already pushed into
// scrollback by scrollUp so captureScreenToScrollback can skip them.
func scrollbackTailOverlap(scrollback, lines [][]Cell) int {
	maxK := len(lines)
	if len(scrollback) < maxK {
		maxK = len(scrollback)
	}
	for k := maxK; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if !linesEqual(scrollback[len(scrollback)-k+i], lines[i]) {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return 0
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
