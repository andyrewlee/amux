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
