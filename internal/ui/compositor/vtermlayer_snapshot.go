package compositor

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/vterm"
)

type VTermSnapshot struct {
	Screen       [][]vterm.Cell
	DirtyLines   []bool
	AllDirty     bool
	CursorX      int
	CursorY      int
	ViewOffset   int
	CursorHidden bool
	ShowCursor   bool
	Width        int
	Height       int
	// Selection state (used during rendering)
	SelActive            bool
	SelStartX, SelStartY int
	SelEndX, SelEndY     int
}

// NewVTermSnapshot creates a snapshot from a VTerm.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshot(term *vterm.VTerm, showCursor bool) *VTermSnapshot {
	return NewVTermSnapshotWithCache(term, showCursor, nil)
}

// NewVTermSnapshotWithCache creates a snapshot from a VTerm, optionally reusing
// lines from a previous snapshot when dirty line tracking allows.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshotWithCache(term *vterm.VTerm, showCursor bool, prev *VTermSnapshot) *VTermSnapshot {
	if term == nil {
		return nil
	}
	defer perf.Time("vterm_snapshot")()

	width := term.Width
	height := term.Height
	if width <= 0 || height <= 0 {
		return nil
	}

	// Copy dirty lines to avoid sharing the backing array
	dirtyLines, allDirty := term.DirtyLines()
	var dirtyLinesCopy []bool
	if dirtyLines != nil {
		if prev != nil && len(prev.DirtyLines) == len(dirtyLines) {
			dirtyLinesCopy = prev.DirtyLines[:len(dirtyLines)]
		} else {
			dirtyLinesCopy = make([]bool, len(dirtyLines))
		}
		copy(dirtyLinesCopy, dirtyLines)
	}

	// Ensure cursor-only changes mark lines dirty for layer rendering.
	// Cursor moves or visibility toggles don't always touch renderDirty,
	// so we force redraw of the previous and current cursor lines when needed.
	if !allDirty && term.ViewOffset == 0 {
		lastCursorX := term.LastCursorX()
		lastCursorY := term.LastCursorY()
		lastShowCursor := term.LastShowCursor()
		lastCursorHidden := term.LastCursorHidden()

		cursorChanged := showCursor != lastShowCursor ||
			term.CursorHidden != lastCursorHidden ||
			term.CursorX != lastCursorX ||
			term.CursorY != lastCursorY

		if cursorChanged {
			// Defensive: ensure dirtyLinesCopy matches screen height.
			if dirtyLinesCopy == nil {
				dirtyLinesCopy = make([]bool, height)
			}
			if lastCursorY >= 0 && lastCursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[lastCursorY] = true
			}
			if term.CursorY >= 0 && term.CursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[term.CursorY] = true
			}
		}
	}

	canReuse := prev != nil && prev.Width == width && prev.Height == height && len(prev.Screen) == height
	useDirty := canReuse &&
		prev.ViewOffset == term.ViewOffset &&
		!allDirty &&
		term.ViewOffset == 0 &&
		dirtyLines != nil &&
		len(dirtyLines) == height

	var screen [][]vterm.Cell
	if useDirty {
		screen = prev.Screen
		if screen == nil || len(screen) != height {
			screen = make([][]vterm.Cell, height)
		}

		visible, _ := term.RenderBuffers()
		for y := 0; y < height; y++ {
			needsCopy := dirtyLines[y]
			if screen[y] == nil || len(screen[y]) != width {
				needsCopy = true
			}
			if !needsCopy {
				continue
			}

			line := screen[y]
			if line == nil || len(line) != width {
				line = vterm.MakeBlankLine(width)
			}
			if y < len(visible) {
				copy(line, visible[y])
			} else {
				for i := range line {
					line[i] = vterm.DefaultCell()
				}
			}
			screen[y] = line
		}
	} else {
		// Full snapshot when dirty tracking isn't usable.
		reuseScreen := prev != nil && prev.Width == width && prev.Height == height && len(prev.Screen) == height
		if reuseScreen {
			screen = term.VisibleScreenInto(prev.Screen)
		} else {
			screen = term.VisibleScreen()
		}
		if len(screen) == 0 {
			return nil
		}
	}

	snap := prev
	if snap == nil {
		snap = &VTermSnapshot{}
	}

	snap.Screen = screen
	snap.DirtyLines = dirtyLinesCopy
	snap.AllDirty = allDirty
	snap.CursorX = term.CursorX
	snap.CursorY = term.CursorY
	snap.ViewOffset = term.ViewOffset
	snap.CursorHidden = term.CursorHidden
	snap.ShowCursor = showCursor
	snap.Width = width
	snap.Height = height
	snap.SelActive = term.SelActive()
	snap.SelStartX = 0
	snap.SelStartY = 0
	snap.SelEndX = 0
	snap.SelEndY = 0

	if snap.SelActive {
		startLine := term.SelStartLine()
		endLine := term.SelEndLine()
		startX := term.SelStartX()
		endX := term.SelEndX()

		// Normalize so start is before end.
		if startLine > endLine || (startLine == endLine && startX > endX) {
			startLine, endLine = endLine, startLine
			startX, endX = endX, startX
		}

		visibleStartLine := term.ScreenYToAbsoluteLine(0)
		visibleEndLine := term.ScreenYToAbsoluteLine(height - 1)

		// If selection is entirely outside viewport, disable selection rendering.
		if endLine < visibleStartLine || startLine > visibleEndLine {
			snap.SelActive = false
		} else {
			if startLine < visibleStartLine {
				snap.SelStartY = 0
				startX = 0
			} else {
				snap.SelStartY = startLine - visibleStartLine
			}

			if endLine > visibleEndLine {
				snap.SelEndY = height - 1
				endX = width - 1
			} else {
				snap.SelEndY = endLine - visibleStartLine
			}

			if startX < 0 {
				startX = 0
			}
			if startX >= width {
				startX = width - 1
			}
			if endX < 0 {
				endX = 0
			}
			if endX >= width {
				endX = width - 1
			}

			snap.SelStartX = startX
			snap.SelEndX = endX
		}
	}

	// Clear dirty state after snapshotting (while still holding the lock)
	// Also update cursor tracking for next frame
	term.ClearDirtyWithCursor(showCursor)

	return snap
}
