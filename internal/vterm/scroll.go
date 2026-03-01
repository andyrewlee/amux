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

	var reusable []Cell
	var reusableSmall [8][]Cell
	reusableLines := reusableSmall[:0]
	if n > len(reusableSmall) {
		reusableLines = make([][]Cell, 0, n)
	}
	for i := 0; i < n; i++ {
		idx := v.ScrollTop + i
		if idx >= 0 && idx < len(v.Screen) {
			reusableLines = append(reusableLines, v.Screen[idx])
		} else {
			reusableLines = append(reusableLines, nil)
		}
	}

	// Capture lines to scrollback (skip alt screen unless explicitly enabled).
	// Copy into recycled buffers so the original lines can be reused for blanks.
	if v.scrollbackEnabled() {
		top := v.ScrollTop
		bottom := top + n
		if bottom > v.ScrollBottom {
			bottom = v.ScrollBottom
		}
		added := 0
		for i := top; i < bottom; i++ {
			if i < len(v.Screen) {
				v.Scrollback = append(v.Scrollback, v.acquireScrollbackCopy(v.Screen[i]))
				added++
			}
		}
		if added > 0 && v.ViewOffset > 0 {
			v.ViewOffset += added
			if v.ViewOffset > len(v.Scrollback) {
				v.ViewOffset = len(v.Scrollback)
			}
		}
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
			reuseIdx := i - (v.ScrollBottom - n)
			if reuseIdx >= 0 && reuseIdx < len(reusableLines) {
				reusable = reusableLines[reuseIdx]
			} else {
				reusable = nil
			}
			v.Screen[i] = blankLineInto(reusable, v.Width)
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

	var reusable []Cell
	var reusableSmall [8][]Cell
	reusableLines := reusableSmall[:0]
	if n > len(reusableSmall) {
		reusableLines = make([][]Cell, 0, n)
	}
	for i := 0; i < n; i++ {
		idx := v.ScrollBottom - n + i
		if idx >= 0 && idx < len(v.Screen) {
			reusableLines = append(reusableLines, v.Screen[idx])
		} else {
			reusableLines = append(reusableLines, nil)
		}
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
			reuseIdx := i - v.ScrollTop
			if reuseIdx >= 0 && reuseIdx < len(reusableLines) {
				reusable = reusableLines[reuseIdx]
			} else {
				reusable = nil
			}
			v.Screen[i] = blankLineInto(reusable, v.Width)
		}
	}
	v.markDirtyRange(v.ScrollTop, v.ScrollBottom-1)
}
