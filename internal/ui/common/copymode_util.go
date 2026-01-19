package common

import (
	"strings"
	"unicode"

	"github.com/andyrewlee/amux/internal/vterm"
)

func rectSelectionText(term *vterm.VTerm, state *CopyState) string {
	if term == nil || state == nil {
		return ""
	}
	width := term.Width
	if width < 1 {
		width = 1
	}
	total := term.TotalLines()
	if total == 0 {
		return ""
	}

	startLine, endLine := state.AnchorLine, state.CursorLine
	startX, endX := state.AnchorX, state.CursorX
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	if startX > endX {
		startX, endX = endX, startX
	}

	startLine = clamp(startLine, 0, total-1)
	endLine = clamp(endLine, 0, total-1)
	startX = clamp(startX, 0, width-1)
	endX = clamp(endX, 0, width-1)

	var result strings.Builder
	for line := startLine; line <= endLine; line++ {
		row := term.LineCells(line)
		for x := startX; x <= endX; x++ {
			r := ' '
			if row != nil && x < len(row) {
				cell := row[x]
				if cell.Width == 0 {
					continue
				}
				r = cell.Rune
				if r == 0 {
					r = ' '
				}
			}
			result.WriteRune(r)
		}
		if line < endLine {
			result.WriteRune('\n')
		}
	}
	return result.String()
}

func lineRunes(term *vterm.VTerm, line int) ([]rune, []int) {
	if term == nil {
		return nil, nil
	}
	cells := term.LineCells(line)
	if cells == nil {
		cells = vterm.MakeBlankLine(max(1, term.Width))
	}
	runes := make([]rune, 0, len(cells))
	runeX := make([]int, 0, len(cells))
	for x, cell := range cells {
		if cell.Width == 0 {
			continue
		}
		r := cell.Rune
		if r == 0 {
			r = ' '
		}
		runes = append(runes, r)
		runeX = append(runeX, x)
	}
	return runes, runeX
}

func runeIndexAtX(runeX []int, x int) int {
	if len(runeX) == 0 {
		return 0
	}
	idx := 0
	for i, rx := range runeX {
		if rx >= x {
			return i
		}
		idx = i + 1
	}
	return idx
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
