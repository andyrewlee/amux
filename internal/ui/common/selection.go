package common

import "github.com/andyrewlee/amux/internal/vterm"

// SelectionState tracks mouse selection state for copy/paste.
type SelectionState struct {
	Active    bool // Selection in progress (mouse button down)?
	StartX    int  // Start column (terminal coordinates)
	StartLine int  // Start row (absolute line number, 0 = first scrollback line)
	EndX      int  // End column
	EndLine   int  // End row (absolute line number)
}

// DragSelect advances an in-progress drag selection toward the pointer at
// (termX, termY) and reports whether an auto-scroll tick loop should be
// (re)started. The caller must hold the owning tab/terminal mutex and must have
// already confirmed sel.Active and a non-nil v.
//
// termX is clamped to [0, termWidth); the unclamped termY drives the scroll
// direction (scrollState.SetDirection) before being clamped to the viewport.
// When the pointer is above or below the viewport, scroll is invoked with +1
// (up into history) or -1 (down toward live) and termY is pinned to the
// matching edge. The clamped termY is mapped to an absolute line via
// screenYToAbs and the selection endpoint is extended there. The clamped termX
// is written to *lastTermX so tick-driven edge extension can reuse it.
//
// The two closures absorb the only per-pane divergence: scroll performs the
// pane's viewport move (center's chat-history-aware scroll vs the sidebar's raw
// ScrollView+note), and screenYToAbs maps a screen row to an absolute line.
// The returned (needTick, gen) come straight from scrollState.NeedsTick(); the
// caller owns scheduling the actual tick command.
func DragSelect(
	v *vterm.VTerm,
	sel *SelectionState,
	scrollState *SelectionScrollState,
	termX, termY, termWidth, termHeight int,
	lastTermX *int,
	scroll func(delta int),
	screenYToAbs func(screenY int) int,
) (needTick bool, gen uint64) {
	if termX < 0 {
		termX = 0
	}
	if termX >= termWidth {
		termX = termWidth - 1
	}

	// Set scroll direction from the unclamped Y before clamping.
	scrollState.SetDirection(termY, termHeight)

	if termY < 0 {
		scroll(1)
		termY = 0
	} else if termY >= termHeight {
		scroll(-1)
		termY = termHeight - 1
	}

	absLine := screenYToAbs(termY)
	ExtendSelection(v, sel, termX, absLine)

	*lastTermX = termX
	return scrollState.NeedsTick()
}

// SelectionScrollTickStep performs one auto-scroll tick of a drag selection:
// it scrolls the viewport one line in scrollState.ScrollDir and extends the
// selection endpoint to the now-exposed viewport edge. The caller must hold the
// owning tab/terminal mutex and must have already validated the tick via
// scrollState.HandleTick (which confirms the generation, direction, and active
// state) plus sel.Active and a non-nil v.
//
// The edge row is the top of the viewport when scrolling up into history and
// the bottom (termHeight-1) when scrolling down toward live; screenYToAbs maps
// it to an absolute line. lastTermX supplies the horizontal endpoint. scroll
// and screenYToAbs carry the same per-pane meaning as in DragSelect. The caller
// owns scheduling the next tick command.
func SelectionScrollTickStep(
	v *vterm.VTerm,
	sel *SelectionState,
	scrollState *SelectionScrollState,
	termHeight int,
	lastTermX int,
	scroll func(delta int),
	screenYToAbs func(screenY int) int,
) {
	scroll(scrollState.ScrollDir)

	edgeY := 0
	if scrollState.ScrollDir < 0 {
		edgeY = termHeight - 1
	}
	absLine := screenYToAbs(edgeY)
	ExtendSelection(v, sel, lastTermX, absLine)
}

// ExtendSelection moves the far endpoint of an in-progress selection to
// (endX, endAbsLine). The anchor is read from the live vterm selection, falling
// back to sel's stored start when the vterm has no selection yet, then written
// back to sel so the two stay in sync. This block is order-sensitive; callers
// must hold the owning tab/terminal mutex. It performs no I/O.
func ExtendSelection(v *vterm.VTerm, sel *SelectionState, endX, endAbsLine int) {
	startX := v.SelStartX()
	startLine := v.SelStartLine()
	if !v.HasSelection() {
		startX = sel.StartX
		startLine = sel.StartLine
	}
	sel.EndX = endX
	sel.EndLine = endAbsLine
	v.SetSelection(startX, startLine, endX, endAbsLine, true, false)
	sel.StartX = startX
	sel.StartLine = startLine
}
