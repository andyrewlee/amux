package vterm

// captureRowsMatch compares lines with captured rows using the current terminal width.
func captureRowsMatch(current, captured [][]Cell, width int) bool {
	if len(current) != len(captured) {
		return false
	}
	for i := range current {
		if !linesEqual(current[i], copyVisibleLine(captured[i], width)) {
			return false
		}
	}
	return true
}

// matchesScrollbackTail returns true if the last len(lines) entries in
// scrollback are cell-identical to lines.
func matchesScrollbackTail(scrollback, lines [][]Cell) bool {
	n := len(lines)
	sb := len(scrollback)
	if sb < n || n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if !linesEqual(scrollback[sb-n+i], lines[i]) {
			return false
		}
	}
	return true
}

// scrollbackTailOverlap returns the length of the longest suffix of scrollback
// that matches a prefix of lines. This detects lines already pushed into
// scrollback by scrollUp so captureScreenToScrollback can skip them.
func scrollbackTailOverlap(scrollback, lines [][]Cell) int {
	maxK := len(lines)
	if len(scrollback) < maxK {
		maxK = len(scrollback)
	}
	for k := maxK; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if !linesEqual(scrollback[len(scrollback)-k+i], lines[i]) {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return 0
}

// frameShiftOverlap returns the longest suffix of oldLines that matches a
// prefix of newLines. A non-zero result indicates the visible frame advanced
// upward and new content appeared below it.
func frameShiftOverlap(oldLines, newLines [][]Cell) int {
	maxK := len(oldLines)
	if len(newLines) < maxK {
		maxK = len(newLines)
	}
	for k := maxK; k > 0; k-- {
		match := true
		for i := 0; i < k; i++ {
			if !linesEqual(oldLines[len(oldLines)-k+i], newLines[i]) {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return 0
}

// linesEqual returns true if two cell slices have identical visible content and
// styles. Scrollback rows are stored trimmed of trailing default cells, so the
// longer row's tail must be all-default for a match — equivalent to the cells
// that were dropped.
func linesEqual(a, b []Cell) bool {
	shorter, longer := a, b
	if len(b) < len(a) {
		shorter, longer = b, a
	}
	for i := range shorter {
		if shorter[i].Rune != longer[i].Rune ||
			shorter[i].GraphemeCluster != longer[i].GraphemeCluster ||
			shorter[i].Width != longer[i].Width ||
			shorter[i].Style != longer[i].Style {
			return false
		}
	}
	def := DefaultCell()
	for i := len(shorter); i < len(longer); i++ {
		if longer[i] != def {
			return false
		}
	}
	return true
}
