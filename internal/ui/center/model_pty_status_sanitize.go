package center

import "github.com/andyrewlee/amux/internal/ui/compositor"

func sanitizeChatCursorCell(snap *compositor.VTermSnapshot, x, y int) {
	if snap == nil || y < 0 || y >= len(snap.Screen) {
		return
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return
	}
	cell := row[x]
	if isSanitizableSyntheticCursorCell(cell) {
		cell.Rune = ' '
		if cell.Width <= 0 {
			cell.Width = 1
		}
	}
	cell.Style.Blink = false
	row[x] = cell
}

func sanitizeStoredChatCursorCell(snap *compositor.VTermSnapshot, x, y int) {
	if snap == nil || y < 0 || y >= len(snap.Screen) {
		return
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return
	}
	cell := row[x]
	if cell.Style.Blink && isSyntheticCursorGlyph(cell.Rune) {
		cell.Rune = ' '
		if cell.Width <= 0 {
			cell.Width = 1
		}
	}
	cell.Style.Blink = false
	row[x] = cell
}
