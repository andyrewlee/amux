package center

import (
	"time"

	"github.com/andyrewlee/amux/internal/ui/compositor"
)

func isRecentLocalChatInput(lastInputAt, now time.Time) bool {
	return !lastInputAt.IsZero() && now.Sub(lastInputAt) <= localInputEchoSuppressWindow
}

func isRecentPromptEditInput(lastPromptInputAt, lastPromptSubmitAt, now time.Time) bool {
	if !isRecentLocalChatInput(lastPromptInputAt, now) {
		return false
	}
	return lastPromptSubmitAt.IsZero() ||
		!isRecentLocalChatInput(lastPromptSubmitAt, now) ||
		lastPromptInputAt.After(lastPromptSubmitAt)
}

func isRecentSubmitPromptInput(lastPromptSubmitAt, now time.Time) bool {
	return isRecentLocalChatInput(lastPromptSubmitAt, now)
}

func isRecentSubmitPromptBeforeOutput(lastPromptSubmitAt, lastVisibleOutput, now time.Time) bool {
	return isRecentSubmitPromptInput(lastPromptSubmitAt, now) &&
		(lastVisibleOutput.IsZero() || !lastVisibleOutput.After(lastPromptSubmitAt))
}

func isNearbySubmitPromptCursor(stableX, stableY, liveX, liveY int) bool {
	if stableY < 0 || liveY < 0 {
		return false
	}
	dy := stableY - liveY
	if dy < 0 {
		dy = -dy
	}
	if dy > 1 {
		return false
	}
	if liveX <= 4 {
		return stableX <= 4
	}
	dx := stableX - liveX
	if dx < 0 {
		dx = -dx
	}
	return dx <= 4
}

func isPlausibleInitialChatCursor(snap *compositor.VTermSnapshot, x, y int) bool {
	return hasChatCursorContextNearPosition(snap, y) ||
		isChatInputCursorPosition(snap, x, y, false)
}

// hasChatCursorContextNearPosition reports whether the cursor is sitting on or
// adjacent to rows with visible content. When restricted output goes idle, this
// lets multiline prompt cursors be re-learned without immediately trusting
// blank control-only cursor jumps.
func hasChatCursorContextNearPosition(snap *compositor.VTermSnapshot, y int) bool {
	if snap == nil || len(snap.Screen) == 0 || y < 0 || y >= len(snap.Screen) {
		return false
	}
	start := y - 1
	if start < 0 {
		start = 0
	}
	end := y + 1
	if end >= len(snap.Screen) {
		end = len(snap.Screen) - 1
	}
	for rowIdx := start; rowIdx <= end; rowIdx++ {
		row := snap.Screen[rowIdx]
		for _, cell := range row {
			if cell.Width == 0 {
				continue
			}
			if cell.Rune != 0 && cell.Rune != ' ' {
				return true
			}
		}
	}
	return false
}
