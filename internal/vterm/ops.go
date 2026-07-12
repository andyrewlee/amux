package vterm

import "github.com/clipperhouse/displaywidth"

// putChar places a character at current cursor position
func (v *VTerm) putChar(r rune) {
	lineY := v.CursorY
	width := displaywidth.Rune(r)

	// Combining characters (width 0) attach to previous cell
	if width == 0 {
		prevX := v.CursorX - 1
		prevY := v.CursorY
		if prevX < 0 && prevY > 0 {
			prevY--
			prevX = v.Width - 1
		}
		// Step back over a wide-char continuation cell to its base cell.
		if prevY >= 0 && prevY < len(v.Screen) {
			line := v.Screen[prevY]
			if prevX > 0 && prevX < len(line) && line[prevX].Width == 0 {
				prevX--
			}
		}
		if prevY >= 0 && prevY < len(v.Screen) && prevX >= 0 && prevX < len(v.Screen[prevY]) {
			cell := &v.Screen[prevY][prevX]
			if cell.Rune != 0 { // never attach to a blank/continuation marker
				base := cell.GraphemeCluster
				if base == "" {
					base = string(cell.Rune)
				}
				cell.GraphemeCluster = base + string(r)
				// VS16 (emoji variation selector) upgrades a narrow base
				// glyph to a full-width emoji: retroactively widen the base
				// cell to 2 columns, mirroring the wide-char path below.
				if r == 0xFE0F && cell.Width == 1 && prevX+1 < v.Width && prevX+1 < len(v.Screen[prevY]) {
					cell.Width = 2

					// Clear any wide char that starts at the continuation
					// position (same guard the wide path uses).
					nextCell := v.Screen[prevY][prevX+1]
					if nextCell.Width == 2 && prevX+2 < v.Width {
						v.Screen[prevY][prevX+2] = DefaultCell()
					}

					v.Screen[prevY][prevX+1] = Cell{
						Rune:  0, // Continuation marker
						Style: cell.Style,
						Width: 0, // Continuation cell
					}

					// The base was placed at CursorX-1 and the cursor already
					// advanced past it by 1; widening it to 2 columns means
					// the cursor must move one more column right. Only when
					// the base cell is immediately left of the cursor — after
					// a wrap to a previous row the cursor has already moved on.
					if prevY == v.CursorY && prevX == v.CursorX-1 {
						v.CursorX++
					}
				}
				v.markDirtyLine(prevY)
			}
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
		v.advanceLineFeed()
	}

	// Normal auto-wrap check
	if v.CursorX >= v.Width {
		v.CursorX = 0
		v.advanceLineFeed()
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

// advanceLineFeed moves the cursor down one row for LF/auto-wrap.
// At the bottom margin it scrolls the region; below the region it moves
// toward the last screen row without scrolling (DEC/xterm semantics).
func (v *VTerm) advanceLineFeed() {
	if v.CursorY >= v.ScrollBottom {
		// Cursor is below the scroll region: never scroll from here.
		if v.CursorY < v.Height-1 {
			v.CursorY++
		}
		return
	}
	v.CursorY++
	if v.CursorY >= v.ScrollBottom {
		v.scrollUp(1)
		v.CursorY = v.ScrollBottom - 1
	}
}

// newline moves cursor down, scrolling if needed
func (v *VTerm) newline() {
	prevX, prevY := v.CursorX, v.CursorY
	v.advanceLineFeed()
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
		if v.CursorX == 0 && v.CursorY == 0 && v.shouldCaptureScreenOnClear() {
			captured := v.captureScreenToScrollback()
			v.preserveScrollbackOnNextClear3 = v.syncActive && captured
		}
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
	case 2, 3: // Entire display (3 normally also clears scrollback)
		// Capture non-blank screen lines to scrollback before erasing,
		// so TUI content (like Claude Code plan mode) is preserved for
		// amux scroll.
		if mode == 2 && v.shouldCaptureScreenOnClear() {
			captured := v.captureScreenToScrollback()
			v.preserveScrollbackOnNextClear3 = v.syncActive && captured
		}
		for y := 0; y < v.Height; y++ {
			if y < len(v.Screen) {
				v.Screen[y] = MakeBlankLine(v.Width)
			}
		}
		if mode == 3 {
			if v.preserveScrollbackOnNextClear3 {
				v.preserveScrollbackOnNextClear3 = false
			} else {
				v.Scrollback = v.Scrollback[:0]
				v.invalidateAltScreenCapture()
			}
		}
		if mode != 2 {
			v.preserveScrollbackOnNextClear3 = false
		}
		v.markDirtyRange(0, v.Height-1)
	}
}

func (v *VTerm) shouldCaptureScreenOnClear() bool {
	return (v.AltScreen && v.AllowAltScreenScrollback) ||
		(!v.AltScreen && v.CaptureNormalScreenOnClear)
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
