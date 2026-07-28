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

func (v *VTerm) clampAltSavedCursor() {
	if v.Width < 1 || v.Height < 1 {
		return
	}
	if v.altCursorX < 0 {
		v.altCursorX = 0
	}
	if v.altCursorX >= v.Width {
		v.altCursorX = v.Width - 1
	}
	if v.altCursorY < 0 {
		v.altCursorY = 0
	}
	if v.altCursorY >= v.Height {
		v.altCursorY = v.Height - 1
	}
}

func (v *VTerm) clampSavedCursor() {
	if v.Width < 1 || v.Height < 1 {
		return
	}
	if v.SavedCursorX < 0 {
		v.SavedCursorX = 0
	}
	if v.SavedCursorX >= v.Width {
		v.SavedCursorX = v.Width - 1
	}
	if v.SavedCursorY < 0 {
		v.SavedCursorY = 0
	}
	if v.SavedCursorY >= v.Height {
		v.SavedCursorY = v.Height - 1
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

// savedCursor is one DECSC slot: the position and style ESC 7 records and
// ESC 8 restores. Each screen buffer owns one.
type savedCursor struct {
	x, y  int
	style Style
}

// enterAltScreen switches to the alternate screen buffer.
//
// saveCursor selects mode 1049's semantics: save the cursor on the way in and
// restore it on the way out. Modes 47 and 1047 pass false — they switch buffers
// and nothing else, which is what a real terminal does and why 1049 exists.
//
// Either way the primary buffer's DECSC slot is stashed, because that slot is
// per-buffer in a real terminal: a full-screen application issuing ESC 7 must
// not overwrite the position the shell saved before it started.
func (v *VTerm) enterAltScreen(saveCursor bool) {
	if v.AltScreen {
		return
	}
	v.AltScreen = true
	v.invalidateAltScreenCapture()
	if saveCursor {
		v.altCursorX = v.CursorX
		v.altCursorY = v.CursorY
	}
	v.stashPrimarySavedCursor()
	v.altScreenBuf = v.Screen
	v.Screen = v.makeScreen(v.Width, v.Height)
	if saveCursor {
		// Home unconditionally, without clamping: under origin mode clampCursor
		// would pull the cursor down to ScrollTop, and 1049 homes to the true
		// origin. The cursor carried over by 47/1047 needs no clamp either — it
		// came from a screen of identical dimensions.
		v.CursorX = 0
		v.CursorY = 0
	}
	v.invalidateRenderCache()
}

// exitAltScreen returns to the main screen buffer.
//
// restoreCursor mirrors enterAltScreen's saveCursor: only mode 1049 puts the
// cursor back where it was before the alternate screen was entered. Modes 47
// and 1047 leave it wherever the application left it.
func (v *VTerm) exitAltScreen(restoreCursor bool) {
	if !v.AltScreen {
		return
	}
	v.AltScreen = false
	v.invalidateAltScreenCapture()
	v.Screen = v.altScreenBuf
	v.altScreenBuf = nil
	if restoreCursor {
		// altCursorX/Y can predate a resize, so the restored position is the one
		// that needs clamping. The cursor 47/1047 carries over is already valid
		// for a screen of these dimensions, and clamping it would move it under
		// origin mode — the same trap enterAltScreen avoids when homing.
		v.CursorX = v.altCursorX
		v.CursorY = v.altCursorY
		v.clampCursor()
	}
	v.restorePrimarySavedCursor()
	v.invalidateRenderCache()
}

// stashPrimarySavedCursor sets the DECSC slot aside on the way into the
// alternate screen and hands the alternate buffer a fresh one at the origin,
// matching how a real terminal gives each buffer its own saved cursor.
func (v *VTerm) stashPrimarySavedCursor() {
	v.primarySavedCursor = savedCursor{x: v.SavedCursorX, y: v.SavedCursorY, style: v.SavedStyle}
	v.inAltSavedCursor = true
	v.SavedCursorX = 0
	v.SavedCursorY = 0
	v.SavedStyle = Style{}
}

// restorePrimarySavedCursor puts the primary buffer's DECSC slot back on the
// way out, discarding whatever the alternate-screen application saved.
//
// The inAltSavedCursor guard matters because the alt-screen flag can also be
// cleared by a tmux reattach (applyPaneModeState) rather than by a mode reset:
// without it, an unpaired exit would install a zeroed slot over a live one.
func (v *VTerm) restorePrimarySavedCursor() {
	if !v.inAltSavedCursor {
		return
	}
	v.SavedCursorX = v.primarySavedCursor.x
	v.SavedCursorY = v.primarySavedCursor.y
	v.SavedStyle = v.primarySavedCursor.style
	v.primarySavedCursor = savedCursor{}
	v.inAltSavedCursor = false
}

// discardStashedSavedCursor drops the stashed primary slot without installing
// it. A tmux reattach can put the terminal on the primary screen without the
// matching mode reset ever arriving, and in that case the restore path
// (restoreFromCapture) reseeds the DECSC slot from the restored cursor. Keeping
// the stale stash around would let a later exitAltScreen overwrite that fresh
// slot with a position from before the detach.
func (v *VTerm) discardStashedSavedCursor() {
	v.primarySavedCursor = savedCursor{}
	v.inAltSavedCursor = false
}

// saveCursor saves cursor position and attributes (DECSC / ESC 7).
func (v *VTerm) saveCursor() {
	v.SavedCursorX = v.CursorX
	v.SavedCursorY = v.CursorY
	v.SavedStyle = v.CurrentStyle
}

// restoreCursor restores cursor position and attributes (DECRC / ESC 8).
func (v *VTerm) restoreCursor() {
	prevX, prevY := v.CursorX, v.CursorY
	v.clampSavedCursor()
	v.CursorX = v.SavedCursorX
	v.CursorY = v.SavedCursorY
	v.CurrentStyle = v.SavedStyle
	v.bumpVersionIfCursorMoved(prevX, prevY)
}
