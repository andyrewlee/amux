package vterm

// scrollUp scrolls the screen up by n lines, capturing to scrollback
// This is THE critical function - lines scroll off into scrollback here
func (v *VTerm) scrollUp(n int) {
	if n <= 0 {
		return
	}
	v.ClearSelection()

	// Clamp n to scroll region height
	regionHeight := v.ScrollBottom - v.ScrollTop
	if n > regionHeight {
		n = regionHeight
	}

	// Capture lines to scrollback (skip alt screen unless explicitly enabled)
	if v.scrollbackEnabled() {
		top := v.ScrollTop
		bottom := top + n
		if bottom > v.ScrollBottom {
			bottom = v.ScrollBottom
		}
		added := 0
		for i := top; i < bottom; i++ {
			if i < len(v.Screen) {
				// Move (not copy) the row: the shift and blank-fill loops
				// below reassign every Screen slot in [ScrollTop, ScrollBottom),
				// so after this append the appended slice is the sole live
				// reference (snapshot/render paths copy cell contents, never
				// retain Screen row headers).
				v.Scrollback = append(v.Scrollback, v.Screen[i])
				added++
			}
		}
		if added > 0 {
			if v.altCapture.tracked && v.altCapture.frameLen > 0 &&
				v.altCapture.dropLen > 0 {
				v.altCapture.endOffset += added
			} else {
				v.invalidateAltScreenCapture()
			}
		}
		v.anchorViewOffsetForAddedLines(added)
		v.trimScrollback()
	}

	// Shift screen content up within scroll region
	for i := v.ScrollTop; i < v.ScrollBottom-n; i++ {
		if i+n < len(v.Screen) {
			v.Screen[i] = v.Screen[i+n]
		}
	}

	// Fill bottom with blank lines
	for i := v.ScrollBottom - n; i < v.ScrollBottom; i++ {
		if i >= 0 && i < len(v.Screen) {
			v.Screen[i] = MakeBlankLine(v.Width)
		}
	}
	v.markDirtyRange(v.ScrollTop, v.ScrollBottom-1)
}

// scrollDown scrolls the screen down by n lines (reverse scroll)
func (v *VTerm) scrollDown(n int) {
	if n <= 0 {
		return
	}

	// Clamp n to scroll region height
	regionHeight := v.ScrollBottom - v.ScrollTop
	if n > regionHeight {
		n = regionHeight
	}

	// Shift screen content down within scroll region
	for i := v.ScrollBottom - 1; i >= v.ScrollTop+n; i-- {
		if i-n >= 0 && i < len(v.Screen) {
			v.Screen[i] = v.Screen[i-n]
		}
	}

	// Fill top with blank lines
	for i := v.ScrollTop; i < v.ScrollTop+n; i++ {
		if i >= 0 && i < len(v.Screen) {
			v.Screen[i] = MakeBlankLine(v.Width)
		}
	}
	v.markDirtyRange(v.ScrollTop, v.ScrollBottom-1)
}
