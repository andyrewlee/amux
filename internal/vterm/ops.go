package vterm

import "github.com/mattn/go-runewidth"

// putChar places a character at current cursor position
func (v *VTerm) putChar(r rune) {
	lineY := v.CursorY
	width := runewidth.RuneWidth(r)

	// Combining characters (width 0) attach to previous cell
	if width == 0 {
		// Find previous cell to attach to
		prevX := v.CursorX - 1
		prevY := v.CursorY
		if prevX < 0 && prevY > 0 {
			prevY--
			prevX = v.Width - 1
		}
		if prevY >= 0 && prevY < len(v.Screen) && prevX >= 0 && prevX < len(v.Screen[prevY]) {
			// Append combining character to previous cell's rune
			// Note: This stores combined as a single rune which works for simple cases
			// For full combining support, Cell.Rune would need to be a string
			cell := &v.Screen[prevY][prevX]
			// Skip if previous cell is a continuation cell (Width==0)
			// For non-continuation cells, we could append combining chars
			// Full support would require storing multiple runes per cell
			_ = cell // Currently no-op for combining characters
		}
		return // Don't advance cursor for combining chars
	}

	// Wide characters: if at last column, wrap first to avoid splitting
	if width == 2 && v.CursorX == v.Width-1 {
		// Put a space in the last column and wrap
		if v.CursorY >= 0 && v.CursorY < len(v.Screen) {
			v.Screen[v.CursorY][v.CursorX] = Cell{
				Rune:  ' ',
				Style: v.CurrentStyle,
				Width: 1,
			}
			v.markDirtyLine(v.CursorY)
		}
		v.CursorX = 0
		v.CursorY++
		if v.CursorY >= v.ScrollBottom {
			v.scrollUp(1)
			v.CursorY = v.ScrollBottom - 1
		}
	}

	// Normal auto-wrap check
	if v.CursorX >= v.Width {
		v.CursorX = 0
		v.CursorY++
		if v.CursorY >= v.ScrollBottom {
			v.scrollUp(1)
			v.CursorY = v.ScrollBottom - 1
		}
	}

	// Place the character
	if v.CursorY >= 0 && v.CursorY < len(v.Screen) &&
		v.CursorX >= 0 && v.CursorX < len(v.Screen[v.CursorY]) {

		// Before placing the character, handle overwriting wide chars
		currentCell := v.Screen[v.CursorY][v.CursorX]

		// If we're overwriting a continuation cell (Width==0), clear the wide char before it
		if currentCell.Width == 0 && v.CursorX > 0 {
			v.Screen[v.CursorY][v.CursorX-1] = DefaultCell()
		}

		// If we're overwriting a wide char (Width==2) with something else, clear its continuation
		if currentCell.Width == 2 && v.CursorX+1 < v.Width {
			v.Screen[v.CursorY][v.CursorX+1] = DefaultCell()
		}

		v.Screen[v.CursorY][v.CursorX] = Cell{
			Rune:  r,
			Style: v.CurrentStyle,
			Width: width,
		}

		// For wide characters, create continuation cell
		if width == 2 && v.CursorX+1 < v.Width {
			// Also clear any wide char that might start at the continuation position
			nextCell := v.Screen[v.CursorY][v.CursorX+1]
			if nextCell.Width == 2 && v.CursorX+2 < v.Width {
				v.Screen[v.CursorY][v.CursorX+2] = DefaultCell()
			}

			v.Screen[v.CursorY][v.CursorX+1] = Cell{
				Rune:  0, // Continuation marker
				Style: v.CurrentStyle,
				Width: 0, // Continuation cell
			}
		}
	}

	v.markDirtyLine(lineY)
	v.markDirtyLine(v.CursorY)

	// Advance cursor by character width
	v.CursorX += width
}

// newline moves cursor down, scrolling if needed
func (v *VTerm) newline() {
	prevX, prevY := v.CursorX, v.CursorY
	v.CursorY++
	if v.CursorY >= v.ScrollBottom {
		v.scrollUp(1)
		v.CursorY = v.ScrollBottom - 1
	}
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// carriageReturn moves cursor to beginning of line
func (v *VTerm) carriageReturn() {
	prevX, prevY := v.CursorX, v.CursorY
	v.CursorX = 0
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// tab moves cursor to next tab stop (every 8 columns)
func (v *VTerm) tab() {
	prevX, prevY := v.CursorX, v.CursorY
	v.CursorX = ((v.CursorX / 8) + 1) * 8
	if v.CursorX >= v.Width {
		v.CursorX = v.Width - 1
	}
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

// backspace moves cursor back one
func (v *VTerm) backspace() {
	prevX, prevY := v.CursorX, v.CursorY
	if v.CursorX > 0 {
		v.CursorX--
	}
	v.bumpVersionIfCursorMoved(prevX, prevY)
}

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

	// Capture lines to scrollback (only when not in alt screen)
	if !v.AltScreen {
		top := v.ScrollTop
		bottom := top + n
		if bottom > v.ScrollBottom {
			bottom = v.ScrollBottom
		}
		added := 0
		for i := top; i < bottom; i++ {
			if i < len(v.Screen) {
				v.Scrollback = append(v.Scrollback, CopyLine(v.Screen[i]))
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

// eraseDisplay clears parts of the display
func (v *VTerm) eraseDisplay(mode int) {
	switch mode {
	case 0: // Cursor to end
		// Clear from cursor to end of line
		if v.CursorY < len(v.Screen) {
			for x := v.CursorX; x < v.Width; x++ {
				if x < len(v.Screen[v.CursorY]) {
					v.Screen[v.CursorY][x] = DefaultCell()
				}
			}
		}
		// Clear all lines below
		for y := v.CursorY + 1; y < v.Height; y++ {
			if y < len(v.Screen) {
				v.Screen[y] = MakeBlankLine(v.Width)
			}
		}
		v.markDirtyRange(v.CursorY, v.Height-1)
	case 1: // Start to cursor
		// Clear all lines above
		for y := 0; y < v.CursorY; y++ {
			if y < len(v.Screen) {
				v.Screen[y] = MakeBlankLine(v.Width)
			}
		}
		// Clear from start of line to cursor
		if v.CursorY < len(v.Screen) {
			for x := 0; x <= v.CursorX && x < v.Width; x++ {
				if x < len(v.Screen[v.CursorY]) {
					v.Screen[v.CursorY][x] = DefaultCell()
				}
			}
		}
		v.markDirtyRange(0, v.CursorY)
	case 2, 3: // Entire display (3 also clears scrollback)
		for y := 0; y < v.Height; y++ {
			if y < len(v.Screen) {
				v.Screen[y] = MakeBlankLine(v.Width)
			}
		}
		if mode == 3 {
			v.Scrollback = v.Scrollback[:0]
		}
		v.markDirtyRange(0, v.Height-1)
	}
}

// eraseLine clears parts of the current line
func (v *VTerm) eraseLine(mode int) {
	if v.CursorY >= len(v.Screen) {
		return
	}

	switch mode {
	case 0: // Cursor to end
		for x := v.CursorX; x < v.Width; x++ {
			if x < len(v.Screen[v.CursorY]) {
				v.Screen[v.CursorY][x] = DefaultCell()
			}
		}
	case 1: // Start to cursor
		for x := 0; x <= v.CursorX && x < v.Width; x++ {
			if x < len(v.Screen[v.CursorY]) {
				v.Screen[v.CursorY][x] = DefaultCell()
			}
		}
	case 2: // Entire line
		v.Screen[v.CursorY] = MakeBlankLine(v.Width)
	}
	v.markDirtyLine(v.CursorY)
}

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

// insertLines inserts n blank lines at cursor, pushing content down
func (v *VTerm) insertLines(n int) {
	if v.CursorY < v.ScrollTop || v.CursorY >= v.ScrollBottom {
		return
	}

	// Clamp n to remaining space in scroll region
	maxN := v.ScrollBottom - v.CursorY
	if n > maxN {
		n = maxN
	}

	// Shift lines down
	for i := v.ScrollBottom - 1; i >= v.CursorY+n; i-- {
		if i < len(v.Screen) && i-n >= 0 {
			v.Screen[i] = v.Screen[i-n]
		}
	}

	// Insert blank lines
	for i := v.CursorY; i < v.CursorY+n && i < v.ScrollBottom; i++ {
		if i < len(v.Screen) {
			v.Screen[i] = MakeBlankLine(v.Width)
		}
	}
	v.markDirtyRange(v.ScrollTop, v.ScrollBottom-1)
}

// deleteLines deletes n lines at cursor, pulling content up
func (v *VTerm) deleteLines(n int) {
	if v.CursorY < v.ScrollTop || v.CursorY >= v.ScrollBottom {
		return
	}

	// Clamp n to remaining space in scroll region
	maxN := v.ScrollBottom - v.CursorY
	if n > maxN {
		n = maxN
	}

	// Shift lines up
	for i := v.CursorY; i < v.ScrollBottom-n; i++ {
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

// insertChars inserts n blank chars at cursor, shifting content right
func (v *VTerm) insertChars(n int) {
	if v.CursorY >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorY]
	normalizeLine(line)

	// Shift right
	for i := v.Width - 1; i >= v.CursorX+n; i-- {
		if i < len(line) && i-n >= 0 {
			line[i] = line[i-n]
		}
	}

	// Insert blanks
	for i := v.CursorX; i < v.CursorX+n && i < v.Width; i++ {
		if i < len(line) {
			line[i] = DefaultCell()
		}
	}
	normalizeLine(line)
	v.markDirtyLine(v.CursorY)
}

// deleteChars deletes n chars at cursor, shifting content left
func (v *VTerm) deleteChars(n int) {
	if v.CursorY >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorY]
	normalizeLine(line)

	// Shift left
	for i := v.CursorX; i < v.Width-n; i++ {
		if i+n < len(line) {
			line[i] = line[i+n]
		}
	}

	// Fill end with blanks
	for i := v.Width - n; i < v.Width; i++ {
		if i >= 0 && i < len(line) {
			line[i] = DefaultCell()
		}
	}
	normalizeLine(line)
	v.markDirtyLine(v.CursorY)
}

// eraseChars erases n chars at cursor (doesn't shift)
func (v *VTerm) eraseChars(n int) {
	if v.CursorY >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorY]

	for i := v.CursorX; i < v.CursorX+n && i < v.Width; i++ {
		if i < len(line) {
			line[i] = DefaultCell()
		}
	}
	normalizeLine(line)
	v.markDirtyLine(v.CursorY)
}

// normalizeLine ensures wide characters (Width==2) and continuation cells (Width==0)
// are consistent after in-place line edits (insert/delete/erase).
func normalizeLine(line []Cell) {
	for i := 0; i < len(line); i++ {
		switch line[i].Width {
		case 0:
			// Continuation without a leading wide cell is invalid.
			if i == 0 || line[i-1].Width != 2 {
				line[i] = DefaultCell()
			}
		case 2:
			// If the continuation cell is missing, drop the wide glyph.
			if i+1 >= len(line) || line[i+1].Width != 0 {
				line[i] = DefaultCell()
			}
		}
	}
}
