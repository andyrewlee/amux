package app

import "github.com/andyrewlee/amux/internal/ui/compositor"

// delegateTerminalCursor moves a terminal pane's in-snapshot cursor onto the
// hardware cursor (via setCursor) when it is visible and in bounds, returning a
// layer whose snapshot has ShowCursor=false so exactly one cursor renders. The
// shallow snapshot copy is intentional: only ShowCursor changes; the screen data
// stays read-only for rendering. Returns termLayer unchanged when there is no
// visible cursor to delegate.
func delegateTerminalCursor(termLayer *compositor.VTermLayer, originX, originY, termW, termH int, setCursor func(x, y int)) *compositor.VTermLayer {
	if termLayer == nil || termLayer.Snap == nil {
		return termLayer
	}
	snap := termLayer.Snap
	if snap.ShowCursor && !snap.CursorHidden && snap.ViewOffset == 0 &&
		snap.CursorX >= 0 && snap.CursorY >= 0 &&
		snap.CursorX < termW && snap.CursorY < termH {
		setCursor(originX+snap.CursorX, originY+snap.CursorY)
		snapCopy := *snap
		snapCopy.ShowCursor = false
		termLayer = compositor.NewVTermLayer(&snapCopy)
	}
	return termLayer
}
