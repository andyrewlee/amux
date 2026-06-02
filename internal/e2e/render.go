package e2e

import (
	"strings"

	"github.com/andyrewlee/amux/internal/vterm"
)

// CellsToASCII converts a vterm screen buffer to ASCII text.
func CellsToASCII(screen [][]vterm.Cell) string {
	if len(screen) == 0 {
		return ""
	}
	lines := make([]string, 0, len(screen))
	for _, row := range screen {
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 {
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			if r > 0x7f {
				b.WriteByte('?')
				continue
			}
			b.WriteRune(r)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}
