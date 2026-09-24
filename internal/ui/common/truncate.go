package common

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// TruncateRightCells truncates s to at most maxCells display cells, appending
// marker when truncation occurs (the marker's width is part of the budget).
// The kept prefix never drops below floorRunes leading runes — an over-wide
// result is possible in that case, matching the rune-floor loops this
// replaces. Grapheme clusters are never split, so unlike a rune-at-a-time
// loop the result can't end in a broken emoji/ZWJ sequence.
func TruncateRightCells(s string, maxCells int, marker string, floorRunes int) string {
	w := ansi.StringWidth(s)
	if maxCells <= 0 || w <= maxCells {
		return s
	}
	budget := maxCells - ansi.StringWidth(marker)
	if budget < 0 {
		budget = 0
	}
	var b strings.Builder
	b.Grow(len(s))
	kept, keptRunes := 0, 0
	rest := s
	for len(rest) > 0 {
		cluster, cw := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if keptRunes >= floorRunes && kept+cw > budget {
			break
		}
		b.WriteString(cluster)
		kept += cw
		keptRunes += utf8.RuneCountInString(cluster)
		rest = rest[len(cluster):]
	}
	return b.String() + marker
}

// TruncateLeftCells truncates s to at most maxCells display cells by removing
// leading content and prepending marker (the marker's width is part of the
// budget). At least floorRunes trailing runes are always kept — the result
// may exceed maxCells in that case, matching the rune-floor loops this
// replaces. Grapheme clusters are never split.
func TruncateLeftCells(s string, maxCells int, marker string, floorRunes int) string {
	w := ansi.StringWidth(s)
	if maxCells <= 0 || w <= maxCells {
		return s
	}
	budget := maxCells - ansi.StringWidth(marker)
	if budget < 0 {
		budget = 0
	}
	totalRunes := utf8.RuneCountInString(s)
	skipped, skippedRunes := 0, 0
	rest := s
	for len(rest) > 0 {
		cluster, cw := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if w-skipped <= budget || totalRunes-skippedRunes <= floorRunes {
			break
		}
		skipped += cw
		skippedRunes += utf8.RuneCountInString(cluster)
		rest = rest[len(cluster):]
	}
	return marker + rest
}
