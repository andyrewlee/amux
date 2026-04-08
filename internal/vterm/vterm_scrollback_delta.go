package vterm

func appendScrollbackDeltaMatchStart(lines, retained, screen [][]Cell) int {
	if len(retained) == 0 {
		return 0
	}
	if len(retained) > len(lines) {
		return -1
	}

	bestStart := -1
	bestScreenPrefix := -1
	lastStart := len(lines) - len(retained)
	for start := 0; start <= lastStart; start++ {
		matched := true
		for i := range retained {
			if !appendScrollbackDeltaLineEqual(retained[i], lines[start+i]) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}

		screenPrefix := appendScrollbackDeltaScreenPrefix(lines[start+len(retained):], screen)
		if screenPrefix > bestScreenPrefix || (screenPrefix == bestScreenPrefix && start > bestStart) {
			bestStart = start
			bestScreenPrefix = screenPrefix
		}
	}
	return bestStart
}

func appendScrollbackDeltaScreenPrefix(lines, screen [][]Cell) int {
	if len(lines) == 0 || len(screen) == 0 {
		return 0
	}
	limit := len(lines)
	if len(screen) < limit {
		limit = len(screen)
	}
	matched := 0
	for i := 0; i < limit; i++ {
		if !appendScrollbackDeltaLineEqual(lines[i], screen[i]) {
			break
		}
		matched++
	}
	return matched
}

func appendScrollbackDeltaVisibleTailOnScreen(lines, screen [][]Cell) int {
	if len(lines) == 0 || len(screen) == 0 {
		return 0
	}
	limit := len(lines)
	if len(screen) < limit {
		limit = len(screen)
	}
	for matched := limit; matched > 0; matched-- {
		if appendScrollbackDeltaScreenPrefix(lines[len(lines)-matched:], screen) == matched {
			return matched
		}
	}
	return 0
}

func appendScrollbackDeltaLineEqual(a, b []Cell) bool {
	lastA := appendScrollbackDeltaLastSignificantCell(a)
	lastB := appendScrollbackDeltaLastSignificantCell(b)
	last := lastA
	if lastB > last {
		last = lastB
	}
	if last < 0 {
		return true
	}
	if last >= len(a) || last >= len(b) {
		return false
	}
	for i := 0; i <= last; i++ {
		if a[i].Rune != b[i].Rune || a[i].Style != b[i].Style || a[i].Width != b[i].Width {
			return false
		}
	}
	return true
}

func appendScrollbackDeltaLastSignificantCell(line []Cell) int {
	for i := len(line) - 1; i >= 0; i-- {
		cell := line[i]
		if cell.Width == 0 {
			return i
		}
		if cell.Rune != ' ' && cell.Rune != 0 {
			return i
		}
	}
	return -1
}
