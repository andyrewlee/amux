package vterm

// LoadPaneCapture replaces the terminal screen + scrollback with a full tmux
// pane capture (scrollback plus the current visible screen). This seeds a
// reattached client with the latest frame before live PTY output resumes.
func (v *VTerm) LoadPaneCapture(data []byte) {
	v.LoadPaneCaptureWithCursorAndModes(data, 0, 0, false, PaneModeState{PreserveExistingState: true})
}

// LoadPaneCaptureWithCursorAndModes replaces the terminal screen + scrollback
// with a full tmux pane capture and applies the accompanying tmux VT mode state
// when one is available.
func (v *VTerm) LoadPaneCaptureWithCursorAndModes(
	data []byte,
	cursorX, cursorY int,
	hasCursor bool,
	modeState PaneModeState,
) {
	v.loadPaneCaptureWithCursor(data, cursorX, cursorY, hasCursor, modeState)
}

func (v *VTerm) loadPaneCaptureWithCursor(
	data []byte,
	cursorX, cursorY int,
	hasCursor bool,
	modeState PaneModeState,
) {
	var tmp *VTerm
	if len(data) > 0 {
		tmp = v.parseCapture(data)
		if tmp == nil {
			return
		}
	}

	// A tmux pane snapshot is a complete frame. If the terminal was detached in
	// the middle of DEC synchronized output, the frozen sync buffers must be
	// cleared before publishing the restored frame.
	v.setSynchronizedOutput(false)
	if v.parser != nil {
		v.parser.Reset()
	}
	v.applyPaneModeState(modeState)
	v.ClearSelection()
	v.ViewOffset = 0
	v.Scrollback = v.Scrollback[:0]
	if tmp != nil {
		v.Scrollback = appendTrimmedRows(v.Scrollback, tmp.Scrollback)
	}
	v.trimScrollback()

	newScreen := make([][]Cell, v.Height)
	for i := 0; i < v.Height; i++ {
		if tmp != nil && i < len(tmp.Screen) {
			newScreen[i] = CopyLine(tmp.Screen[i])
			continue
		}
		newScreen[i] = MakeBlankLine(v.Width)
	}
	v.Screen = newScreen
	if tmp != nil {
		v.CurrentStyle = tmp.CurrentStyle
	} else {
		v.CurrentStyle = Style{}
	}
	if hasCursor {
		v.CursorX = cursorX
		v.CursorY = cursorY
	} else if tmp != nil {
		// Full-pane restore is authoritative; when tmux omits explicit cursor
		// metadata, use the cursor implied by the restored frame instead of
		// reusing stale coordinates from the detached terminal.
		v.CursorX = tmp.CursorX
		v.CursorY = tmp.CursorY
	} else {
		v.CursorX = 0
		v.CursorY = 0
	}
	v.clampCursor()
	v.SavedCursorX = v.CursorX
	v.SavedCursorY = v.CursorY
	v.SavedStyle = v.CurrentStyle
	if v.AltScreen {
		v.invalidateAltScreenCapture()
		v.trackRestoredAltScreenFrame()
	} else {
		v.invalidateAltScreenCapture()
	}
	v.invalidateRenderCache()
	v.ensureRenderCache(v.Height)
}

func (v *VTerm) applyPaneModeState(modeState PaneModeState) {
	previousScreen := copyScreenLines(v.Screen)
	previousAltScreenBuf := copyScreenLines(v.altScreenBuf)
	previousAltCursorX := v.altCursorX
	previousAltCursorY := v.altCursorY
	if !modeState.HasState {
		if modeState.PreserveExistingState {
			return
		}
		v.AltScreen = false
		v.altScreenBuf = nil
		v.altCursorX = 0
		v.altCursorY = 0
		v.discardStashedSavedCursor()
		v.ScrollTop = 0
		v.ScrollBottom = v.Height
		v.OriginMode = false
		v.CursorHidden = false
		return
	}
	if modeState.AltScreen {
		v.AltScreen = true
		switch {
		case previousAltScreenBuf != nil:
			v.altScreenBuf = previousAltScreenBuf
		case previousScreen != nil && !isBlankScreen(previousScreen):
			// tmux does not expose the hidden main-screen viewport while a pane
			// is actively in alt-screen mode, so only reuse local main-screen
			// state we already had. Fresh restores fall back to a blank buffer
			// rather than fabricating one from ordinary scrollback history.
			v.altScreenBuf = previousScreen
		default:
			v.altScreenBuf = v.makeScreen(v.Width, v.Height)
		}
		if modeState.HasAltSavedCursor {
			v.altCursorX = modeState.AltSavedCursorX
			v.altCursorY = modeState.AltSavedCursorY
		} else if previousAltScreenBuf != nil {
			v.altCursorX = previousAltCursorX
			v.altCursorY = previousAltCursorY
		} else {
			v.altCursorX = 0
			v.altCursorY = 0
		}
		v.clampAltSavedCursor()
	} else {
		v.AltScreen = false
		v.altScreenBuf = nil
		v.altCursorX = 0
		v.altCursorY = 0
		v.discardStashedSavedCursor()
	}
	scrollTop := modeState.ScrollTop
	scrollBottom := modeState.ScrollBottom
	if scrollTop < 0 || scrollTop >= v.Height || scrollBottom <= scrollTop || scrollBottom > v.Height {
		scrollTop = 0
		scrollBottom = v.Height
	}
	v.ScrollTop = scrollTop
	v.ScrollBottom = scrollBottom
	v.OriginMode = modeState.OriginMode
	v.CursorHidden = modeState.CursorHidden
}

func copyScreenLines(lines [][]Cell) [][]Cell {
	if lines == nil {
		return nil
	}
	copied := make([][]Cell, len(lines))
	for i, line := range lines {
		copied[i] = CopyLine(line)
	}
	return copied
}

func (v *VTerm) parseCapture(data []byte) *VTerm {
	if len(data) == 0 {
		return nil
	}
	return parseCaptureWithSize(data, v.Width, v.Height)
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
