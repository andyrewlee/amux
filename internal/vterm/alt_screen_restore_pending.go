package vterm

func (v *VTerm) transitionPendingRestoredAltScreenCapture(lines [][]Cell) int {
	if len(v.altScreenRestorePending) == 0 || len(lines) == 0 {
		return 0
	}
	overlap := frameShiftOverlap(v.altScreenRestorePending, lines)
	minLen := len(v.altScreenRestorePending)
	if len(lines) < minLen {
		minLen = len(lines)
	}
	if overlap <= 0 || overlap*2 < minLen {
		return 0
	}
	scrolledOff := len(v.altScreenRestorePending) - overlap
	if scrolledOff <= 0 {
		return 0
	}

	preserved := v.altScreenRestorePending[:scrolledOff]
	overlapTail := scrollbackTailOverlap(v.Scrollback, preserved)
	added := 0
	for _, line := range preserved[overlapTail:] {
		v.Scrollback = append(v.Scrollback, CopyLine(line))
		added++
	}
	return added
}

func (v *VTerm) trackRestoredAltScreenFrame() {
	lines := v.visibleCaptureFrame()
	if len(lines) == 0 {
		v.clearPendingRestoredAltScreenCapture()
		return
	}
	v.altScreenRestorePending = lines
}

func (v *VTerm) matchesPendingRestoredAltScreenCapture(lines [][]Cell) bool {
	if len(v.altScreenRestorePending) == 0 {
		return false
	}
	return captureRowsMatch(lines, v.altScreenRestorePending, v.Width)
}

func (v *VTerm) clearPendingRestoredAltScreenCapture() {
	v.altScreenRestorePending = nil
}
