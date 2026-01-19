package vterm

func (v *VTerm) clampCursor() {
	if v.CursorX < 0 {
		v.CursorX = 0
	}
	if v.CursorX >= v.Width {
		v.CursorX = v.Width - 1
	}

	if v.OriginMode {
		if v.CursorY < v.ScrollTop {
			v.CursorY = v.ScrollTop
		}
		if v.CursorY >= v.ScrollBottom {
			v.CursorY = v.ScrollBottom - 1
		}
		return
	}

	if v.CursorY < 0 {
		v.CursorY = 0
	}
	if v.CursorY >= v.Height {
		v.CursorY = v.Height - 1
	}
}

// setCursorPos sets cursor position (1-indexed input, converts to 0-indexed)
func (v *VTerm) setCursorPos(row, col int) {
	prevX, prevY := v.CursorX, v.CursorY
	if v.OriginMode {
		v.CursorY = v.ScrollTop + row - 1
		v.CursorX = col - 1
		v.clampCursor()
		v.bumpVersionIfCursorMoved(prevX, prevY)
		return
	}

	v.CursorY = row - 1
	v.CursorX = col - 1
	v.clampCursor()
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// moveCursor moves cursor relative to current position
func (v *VTerm) moveCursor(dy, dx int) {
	prevX, prevY := v.CursorX, v.CursorY
	v.CursorX += dx
	v.CursorY += dy

	v.clampCursor()
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// setScrollRegion sets the scrolling region (1-indexed input)
func (v *VTerm) setScrollRegion(top, bottom int) {
	prevX, prevY := v.CursorX, v.CursorY
	t := top - 1
	b := bottom

	if t < 0 {
		t = 0
	}
	if b > v.Height {
		b = v.Height
	}
	if t >= b {
		return
	}

	v.ScrollTop = t
	v.ScrollBottom = b
	v.CursorX = 0
	if v.OriginMode {
		v.CursorY = v.ScrollTop
	} else {
		v.CursorY = 0
	}
	v.clampCursor()
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// enterAltScreen switches to alternate screen buffer
func (v *VTerm) enterAltScreen() {
	if v.AltScreen {
		return
	}
	v.AltScreen = true
	v.altCursorX = v.CursorX
	v.altCursorY = v.CursorY
	v.altScreenBuf = v.Screen
	v.Screen = v.makeScreen(v.Width, v.Height)
	v.CursorX = 0
	v.CursorY = 0
	v.invalidateRenderCache()
}

// exitAltScreen returns to main screen buffer
func (v *VTerm) exitAltScreen() {
	if !v.AltScreen {
		return
	}
	v.AltScreen = false
	v.Screen = v.altScreenBuf
	v.altScreenBuf = nil
	v.CursorX = v.altCursorX
	v.CursorY = v.altCursorY
	v.invalidateRenderCache()
}

// saveCursor saves cursor position and attributes
func (v *VTerm) saveCursor() {
	v.SavedCursorX = v.CursorX
	v.SavedCursorY = v.CursorY
	v.SavedStyle = v.CurrentStyle
}

// restoreCursor restores cursor position and attributes
func (v *VTerm) restoreCursor() {
	prevX, prevY := v.CursorX, v.CursorY
	v.CursorX = v.SavedCursorX
	v.CursorY = v.SavedCursorY
	v.CurrentStyle = v.SavedStyle
	v.bumpVersionIfCursorMoved(prevX, prevY)
}
