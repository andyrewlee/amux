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
