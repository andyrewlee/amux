package vterm

func copyLineInto(dst, src []Cell) []Cell {
	if cap(dst) < len(src) {
		dst = make([]Cell, len(src))
	} else {
		dst = dst[:len(src)]
	}
	copy(dst, src)
	return dst
}

func (v *VTerm) acquireScrollbackCopy(src []Cell) []Cell {
	if shared := v.compressScrollbackLine(src); shared != nil {
		return shared
	}

	var dst []Cell
	n := len(v.scrollbackRecycle)
	if n > 0 {
		dst = v.scrollbackRecycle[n-1]
		v.scrollbackRecycle = v.scrollbackRecycle[:n-1]
	}
	return copyLineInto(dst, src)
}

func (v *VTerm) recycleScrollbackLine(line []Cell) {
	if line == nil || len(v.scrollbackRecycle) >= scrollbackRecycleMax || v.isSharedBlankLine(line) {
		return
	}
	v.scrollbackRecycle = append(v.scrollbackRecycle, line[:0])
}

func (v *VTerm) compressScrollbackLine(src []Cell) []Cell {
	if !isCompressibleScrollbackLine(src) {
		return nil
	}
	return v.sharedBlankLine(len(src))
}

func isCompressibleScrollbackLine(line []Cell) bool {
	for _, cell := range line {
		if cell.Style != (Style{}) {
			return false
		}
		if cell.Width != 1 {
			return false
		}
		if cell.Rune != ' ' && cell.Rune != 0 {
			return false
		}
	}
	return true
}

func (v *VTerm) sharedBlankLine(width int) []Cell {
	if width <= 0 {
		return nil
	}
	if cap(v.scrollbackSharedBlank) < width {
		v.scrollbackSharedBlank = make([]Cell, width)
	}
	v.scrollbackSharedBlank = blankLineInto(v.scrollbackSharedBlank, width)
	return v.scrollbackSharedBlank[:width:width]
}

func (v *VTerm) isSharedBlankLine(line []Cell) bool {
	if len(line) == 0 || len(v.scrollbackSharedBlank) == 0 {
		return false
	}
	return &line[0] == &v.scrollbackSharedBlank[0]
}

func blankLineInto(line []Cell, width int) []Cell {
	if cap(line) < width {
		line = make([]Cell, width)
	} else {
		line = line[:width]
	}
	blank := DefaultCell()
	for i := range line {
		line[i] = blank
	}
	return line
}
