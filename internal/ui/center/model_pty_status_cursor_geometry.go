package center

import "github.com/andyrewlee/amux/internal/ui/compositor"

// isChatInputCursorPosition reports whether the cursor is in the visible chat
// input area, using a stricter bottom-band heuristic during active output.
func isChatInputCursorPosition(snap *compositor.VTermSnapshot, x, y int, allowFullViewport bool) bool {
	if snap == nil || snap.ViewOffset != 0 {
		return false
	}
	if y < 0 || y >= len(snap.Screen) {
		return false
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return false
	}
	if allowFullViewport {
		return true
	}
	height := len(snap.Screen)
	if height <= 4 {
		return true
	}
	band := height / 4
	if band < 2 {
		band = 2
	}
	return y >= height-band
}

// isRenderableChatCursorPosition returns whether a live terminal cursor is
// sane enough to adopt as the chat input cursor.
func isRenderableChatCursorPosition(
	snap *compositor.VTermSnapshot,
	x, y int,
	allowFullViewport bool,
	allowBlankCorner bool,
) bool {
	if !isChatInputCursorPosition(snap, x, y, allowFullViewport) {
		return false
	}
	if isSuspiciousBottomEdgeCornerCursor(snap, x, y) && !allowBlankCorner {
		return false
	}
	return true
}

// isStoredChatCursorPosition returns whether a stored chat cursor still fits in
// the current terminal viewport and remains inside the input section.
func isStoredChatCursorPosition(snap *compositor.VTermSnapshot, x, y int, allowFullViewport bool) bool {
	if snap == nil {
		return false
	}
	if snap.ViewOffset != 0 {
		return x >= 0 && y >= 0
	}
	if y < 0 || y >= len(snap.Screen) {
		return false
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return false
	}
	return isChatInputCursorPosition(snap, x, y, allowFullViewport)
}

// isBottomEdgeCornerPosition reports whether (x,y) is either bottom-left or
// bottom-right corner of the snapshot viewport.
func isBottomEdgeCornerPosition(snap *compositor.VTermSnapshot, x, y int) bool {
	if snap == nil || len(snap.Screen) == 0 {
		return false
	}
	lastY := len(snap.Screen) - 1
	if y != lastY {
		return false
	}
	row := snap.Screen[lastY]
	if len(row) == 0 {
		return false
	}
	lastX := len(row) - 1
	return x == 0 || x == lastX
}

// isSuspiciousBottomEdgeCornerCursor reports a common PTY cursor artifact where
// cursor state lands on an empty corner cell on the bottom row.
func isSuspiciousBottomEdgeCornerCursor(snap *compositor.VTermSnapshot, x, y int) bool {
	if !isBottomEdgeCornerPosition(snap, x, y) {
		return false
	}
	row := snap.Screen[len(snap.Screen)-1]
	cell := row[x]
	return cell.Rune == 0 || cell.Rune == ' '
}
