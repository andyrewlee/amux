package vterm

import "github.com/andyrewlee/amux/internal/logging"

// captureScreenToScrollback copies the visible screen frame into the
// scrollback buffer. It trims leading/trailing blank rows and clips each row
// to the current terminal width so only what was actually visible is stored.
// This is called before selected full-screen redraw clears so TUI/chat content
// is preserved for amux scroll-back. A dedup check avoids storing identical
// consecutive frames. It reports whether there was a visible frame worth
// preserving, even if that frame was already present in scrollback.
func (v *VTerm) captureScreenToScrollback() bool {
	lines := v.visibleCaptureFrame()
	if len(lines) == 0 {
		v.clearPendingRestoredAltScreenCapture()
		return false
	}
	oldViewOffset := v.ViewOffset
	if v.matchesPendingRestoredAltScreenCapture(lines) {
		return true
	}
	pendingAdded := v.transitionPendingRestoredAltScreenCapture(lines)
	v.clearPendingRestoredAltScreenCapture()
	if matched, dedupRemoved := v.matchesTrackedAltScreenCapture(lines); matched {
		if oldViewOffset > 0 {
			v.adjustAnchoredViewOffset(pendingAdded - dedupRemoved)
		}
		v.trimScrollback()
		return true
	}
	removed, dropped, transitioned := v.transitionTrackedAltScreenCapture(lines)
	if !transitioned {
		removed, dropped = v.dropTrackedAltScreenCapture()
	}

	deductOffset := func() {
		if oldViewOffset <= 0 {
			return
		}
		v.adjustAnchoredViewOffset(pendingAdded - removed)
	}

	// Dedup: skip if these lines match the tail of scrollback
	if matchesScrollbackTail(v.Scrollback, lines) {
		v.altCapture.frameLen = len(lines)
		v.altCapture.dropLen = 0
		v.altCapture.tracked = len(dropped) > 0 && captureRowsMatch(lines, dropped, v.Width)
		if v.altCapture.tracked {
			v.altCapture.dropLen = len(lines)
		}
		deductOffset()
		v.trimScrollback()
		return true
	}

	// Partial overlap detection — skip lines already in scrollback from scrollUp
	overlap := scrollbackTailOverlap(v.Scrollback, lines)

	added := 0
	for _, line := range lines[overlap:] {
		v.Scrollback = append(v.Scrollback, CopyLine(line))
		added++
	}
	v.altCapture.frameLen = len(lines)
	v.altCapture.dropLen = added
	v.altCapture.tracked = true
	if oldViewOffset > 0 {
		v.adjustAnchoredViewOffset(pendingAdded - removed + added)
	}
	v.trimScrollback()
	return true
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

// matchesTrackedAltScreenCapture checks if lines match the reserved
// alt-screen content. Tracked captures may no longer be at the scrollback tail
// if scrollUp has appended lines after them (tracked by
// altScreenCaptureEndOffset); untracked captures must still match the tail.
func (v *VTerm) matchesTrackedAltScreenCapture(lines [][]Cell) (bool, int) {
	if v.altCapture.frameLen <= 0 || v.altCapture.frameLen != len(lines) {
		return false, 0
	}
	if !v.altCapture.tracked {
		if matchesScrollbackTail(v.Scrollback, lines) {
			return true, 0
		}
		return false, 0
	}
	total := v.altCapture.frameLen + v.altCapture.endOffset
	sb := len(v.Scrollback)
	if sb < total {
		v.altCapture.resetInvalid("scrollback shrank below tracked frame")
		return false, 0
	}
	captureStart := sb - total
	for i := 0; i < v.altCapture.frameLen; i++ {
		if !linesEqual(v.Scrollback[captureStart+i], lines[i]) {
			return false, 0
		}
	}

	// Match confirmed. If redraws scrolled rows off the top while leaving the
	// visible frame unchanged, fold those rows back in front of the tracked
	// frame before deduping. Otherwise repeated redraws can keep the tracked
	// frame ahead of newer history and append the same off-screen rows again.
	removed := v.foldTrackedAltScreenTrailing(captureStart)
	return true, removed
}

// foldTrackedAltScreenTrailing normalizes scrollUp rows that accumulated after
// a tracked frame by moving them into transcript order ahead of that frame.
// Any prefix that already matches the existing history tail is dropped.
func (v *VTerm) foldTrackedAltScreenTrailing(captureStart int) int {
	trailing := v.altCapture.endOffset
	if trailing <= 0 {
		return 0
	}

	captureEnd := captureStart + v.altCapture.frameLen
	trailingStart := captureEnd
	if trailingStart < 0 || trailingStart > len(v.Scrollback) || trailingStart+trailing > len(v.Scrollback) {
		logging.Warn("alt-screen capture: trailing range out of bounds (start=%d trailing=%d scrollback=%d)",
			trailingStart, trailing, len(v.Scrollback))
		v.altCapture.endOffset = 0
		return 0
	}

	before := v.Scrollback[:captureStart]
	trailingLines := v.Scrollback[trailingStart : trailingStart+trailing]
	captureLines := v.Scrollback[captureStart:captureEnd]
	overlap := scrollbackTailOverlap(before, trailingLines)

	reordered := make([][]Cell, 0, len(before)+len(trailingLines)-overlap+len(captureLines))
	reordered = append(reordered, before...)
	reordered = append(reordered, trailingLines[overlap:]...)
	reordered = append(reordered, captureLines...)
	v.Scrollback = reordered
	v.altCapture.endOffset = 0

	return overlap
}

// dropTrackedAltScreenCapture removes the tracked suffix for the previously
// reserved alt-screen frame from scrollback. With altScreenCaptureEndOffset,
// the tracked suffix may be in the middle of scrollback (not at the tail), so
// we remove from its tracked position and preserve any overlap prefix plus
// trailing scrollUp lines.
func (v *VTerm) dropTrackedAltScreenCapture() (int, [][]Cell) {
	if v.altCapture.frameLen <= 0 || !v.altCapture.tracked {
		v.altCapture.reset()
		return 0, nil
	}
	if v.altCapture.dropLen <= 0 || v.altCapture.dropLen > v.altCapture.frameLen {
		v.altCapture.resetInvalid("drop length out of range")
		return 0, nil
	}
	total := v.altCapture.frameLen + v.altCapture.endOffset
	if len(v.Scrollback) < total {
		v.altCapture.resetInvalid("scrollback shrank below tracked frame")
		return 0, nil
	}
	captureStart := len(v.Scrollback) - total
	captureEnd := captureStart + v.altCapture.frameLen
	dropStart := captureEnd - v.altCapture.dropLen

	// Copy the removed rows so the returned slice doesn't alias the
	// Scrollback backing array — a subsequent append could overwrite it.
	src := v.Scrollback[dropStart:captureEnd]
	removedRows := make([][]Cell, len(src))
	copy(removedRows, src)
	removed := v.altCapture.dropLen

	// Remove the tracked suffix from the frame while preserving any overlapping
	// prefix that was already in scrollback and any trailing scrollUp lines.
	v.Scrollback = append(v.Scrollback[:dropStart], v.Scrollback[captureEnd:]...)
	v.altCapture.frameLen = 0
	v.altCapture.dropLen = 0
	v.altCapture.tracked = false

	// Dedup scrollUp trailing lines against pre-capture scrollback.
	// After removal, trailing lines are at [dropStart, dropStart+endOffset).
	dedupRemoved := v.dedupScrollUpTrailing(dropStart)
	removed += dedupRemoved
	v.altCapture.endOffset = 0

	return removed, removedRows
}

// transitionTrackedAltScreenCapture preserves rows that genuinely scrolled off
// the top of the previous tracked frame when the next frame is largely a
// suffix->prefix shift of it. This keeps transcript-style fullscreen redraws
// (for example Claude review output) from dropping earlier text on each erase.
func (v *VTerm) transitionTrackedAltScreenCapture(lines [][]Cell) (int, [][]Cell, bool) {
	if len(lines) == 0 || !v.altCapture.tracked || v.altCapture.frameLen <= 0 {
		return 0, nil, false
	}
	captureStart, captureEnd, dropStart, oldLines, ok := v.trackedAltScreenCapture()
	if !ok {
		return 0, nil, false
	}
	_ = captureStart

	overlap := frameShiftOverlap(oldLines, lines)
	minLen := len(oldLines)
	if len(lines) < minLen {
		minLen = len(lines)
	}
	if overlap <= 0 || overlap*2 < minLen {
		return 0, nil, false
	}
	scrolledOff := len(oldLines) - overlap
	if scrolledOff <= 0 {
		return 0, nil, false
	}

	overlapPrefixLen := len(oldLines) - v.altCapture.dropLen
	if overlapPrefixLen > scrolledOff {
		overlapPrefixLen = scrolledOff
	}
	preserveRows := oldLines[overlapPrefixLen:scrolledOff]
	coveredByTrailing := 0
	if v.altCapture.endOffset > 0 {
		trailingStart := captureEnd
		trailingEnd := trailingStart + v.altCapture.endOffset
		if trailingStart >= 0 && trailingEnd <= len(v.Scrollback) {
			coveredByTrailing = scrollbackTailOverlap(v.Scrollback[trailingStart:trailingEnd], preserveRows)
		}
	}
	preserveAdded := len(preserveRows) - coveredByTrailing
	if preserveAdded > v.altCapture.dropLen {
		preserveAdded = v.altCapture.dropLen
	}
	removeStart := dropStart + preserveAdded
	if removeStart > captureEnd {
		removeStart = captureEnd
	}

	src := v.Scrollback[removeStart:captureEnd]
	removedRows := make([][]Cell, len(src))
	copy(removedRows, src)
	removed := len(src)

	v.Scrollback = append(v.Scrollback[:removeStart], v.Scrollback[captureEnd:]...)
	v.altCapture.frameLen = 0
	v.altCapture.dropLen = 0
	v.altCapture.tracked = false

	dedupRemoved := v.dedupScrollUpTrailing(removeStart)
	removed += dedupRemoved
	v.altCapture.endOffset = 0

	return removed, removedRows, true
}

func (v *VTerm) trackedAltScreenCapture() (int, int, int, [][]Cell, bool) {
	if v.altCapture.frameLen <= 0 || !v.altCapture.tracked {
		return 0, 0, 0, nil, false
	}
	if v.altCapture.dropLen <= 0 || v.altCapture.dropLen > v.altCapture.frameLen {
		v.altCapture.resetInvalid("drop length out of range")
		return 0, 0, 0, nil, false
	}
	total := v.altCapture.frameLen + v.altCapture.endOffset
	if len(v.Scrollback) < total {
		v.altCapture.resetInvalid("scrollback shrank below tracked frame")
		return 0, 0, 0, nil, false
	}
	captureStart := len(v.Scrollback) - total
	captureEnd := captureStart + v.altCapture.frameLen
	dropStart := captureEnd - v.altCapture.dropLen
	return captureStart, captureEnd, dropStart, v.Scrollback[captureStart:captureEnd], true
}

// dedupScrollUpTrailing removes scrollUp lines from the scrollback that
// duplicate content already present in the pre-capture scrollback region
// (scrollback[:preCaptureLen]). This is used after dropping or transitioning a
// tracked frame, where the trailing scrollUp rows already belong after the
// remaining history and only an overlap against that history needs pruning.
func (v *VTerm) dedupScrollUpTrailing(preCaptureLen int) int {
	trailing := v.altCapture.endOffset
	if trailing <= 0 {
		v.altCapture.endOffset = 0
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
	v.altCapture.endOffset = trailing - overlap
	return overlap
}

func (v *VTerm) invalidateAltScreenCapture() {
	v.invalidateTrackedAltScreenCapture()
	v.clearPendingRestoredAltScreenCapture()
}

func (v *VTerm) invalidateTrackedAltScreenCapture() {
	v.altCapture.reset()
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

// frameShiftOverlap returns the longest suffix of oldLines that matches a
// prefix of newLines. A non-zero result indicates the visible frame advanced
// upward and new content appeared below it.
func frameShiftOverlap(oldLines, newLines [][]Cell) int {
	maxK := len(oldLines)
	if len(newLines) < maxK {
		maxK = len(newLines)
	}
	for k := maxK; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if !linesEqual(oldLines[len(oldLines)-k+i], newLines[i]) {
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

// linesEqual returns true if two cell slices have identical visible content and
// styles.
func linesEqual(a, b []Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Rune != b[i].Rune ||
			a[i].GraphemeCluster != b[i].GraphemeCluster ||
			a[i].Width != b[i].Width ||
			a[i].Style != b[i].Style {
			return false
		}
	}
	return true
}
