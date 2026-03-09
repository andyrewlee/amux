package center

import (
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

func isSyntheticCursorGlyph(r rune) bool {
	switch r {
	case '█', '▌', '▐', '▍', '▎', '▋', '▊', '▉', '▮':
		return true
	default:
		return false
	}
}

func isSanitizableSyntheticCursorCell(cell vterm.Cell) bool {
	if cell.Rune == '█' {
		return true
	}
	return isSyntheticCursorGlyph(cell.Rune) && cell.Style.Blink
}

func hasStyledSyntheticCursorAppearance(style vterm.Style) bool {
	return style.Reverse ||
		style.Hidden ||
		style.Bold ||
		style.Dim ||
		style.Italic ||
		style.Underline ||
		style.Strike ||
		style.Fg.Type != vterm.ColorDefault ||
		style.Bg.Type != vterm.ColorDefault
}

func isAppOwnedCursorCell(cell vterm.Cell, allowStaticFullBlock, allowSteadyBar bool) bool {
	if cell.Width == 0 {
		return false
	}
	if cell.Style.Blink && isSyntheticCursorGlyph(cell.Rune) {
		return true
	}
	if allowSteadyBar &&
		cell.Rune != '█' &&
		isSyntheticCursorGlyph(cell.Rune) &&
		hasStyledSyntheticCursorAppearance(cell.Style) {
		return true
	}
	return allowStaticFullBlock && cell.Rune == '█'
}

func hasChatOwnedCursorGlyph(
	snap *compositor.VTermSnapshot,
	liveX int,
	liveY int,
	stableCursorSet bool,
	stableX int,
	stableY int,
) bool {
	if hasSyntheticCursorGlyphAt(snap, liveX, liveY, true, true) {
		return true
	}
	return stableCursorSet && hasSyntheticCursorGlyphAt(snap, stableX, stableY, false, false)
}

func hasSyntheticCursorGlyphAt(
	snap *compositor.VTermSnapshot,
	x, y int,
	allowStaticFullBlock bool,
	allowSteadyBar bool,
) bool {
	if snap == nil || y < 0 || y >= len(snap.Screen) {
		return false
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return false
	}
	cell := row[x]
	return isAppOwnedCursorCell(cell, allowStaticFullBlock, allowSteadyBar)
}
