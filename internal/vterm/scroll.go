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

	// Capture lines to scrollback (skip alt screen unless explicitly enabled;
	// only a top-anchored region feeds scrollback per xterm/DEC semantics —
	// lines scrolled off a partial region's top margin are discarded, not
	// saved, since they never reached the top of the physical screen).
	if v.scrollbackEnabled() && v.ScrollTop == 0 {
		top := v.ScrollTop
		bottom := top + n
		if bottom > v.ScrollBottom {
			bottom = v.ScrollBottom
		}
		if bottom > len(v.Screen) {
			bottom = len(v.Screen)
		}
		// Move (not copy) the rows: the shift and blank-fill loops below
		// reassign every Screen slot in [ScrollTop, ScrollBottom), so after
		// this append the appended slice is the sole live reference
		// (snapshot/render paths copy cell contents, never retain Screen row
		// headers).
		displaced := v.Screen[top:bottom]
		// Synthesized history only, and only for a scroll that displaces the
		// whole region at once: that is how tmux renders a repainting chat
		// agent's scroll, and it is the only case where the displaced block is
		// a frame whose tail is the agent's prompt box (see chrome_filter.go).
		// A smaller scroll displaces rows from the middle of the transcript,
		// where a trailing rule is content rather than chrome.
		if v.AltScreen && v.AllowAltScreenScrollback && n >= regionHeight-1 {
			displaced = trimCapturedChrome(displaced, v.Width)
		}
		added := 0
		for _, line := range displaced {
			v.Scrollback = append(v.Scrollback, line)
			added++
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
