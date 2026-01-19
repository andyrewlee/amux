package vterm

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
