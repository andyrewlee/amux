package common

import (
	"github.com/andyrewlee/amux/internal/vterm"
)

// CopyState tracks cursor and selection while in copy mode.
type CopyState struct {
	Initialized        bool
	CursorX            int
	CursorLine         int
	AnchorX            int
	AnchorLine         int
	Selecting          bool
	Rectangular        bool
	SearchActive       bool
	SearchBackward     bool
	SearchQuery        string
	LastSearch         string
	LastSearchBackward bool
}

// InitCopyState initializes the copy cursor and selection highlight.
func InitCopyState(term *vterm.VTerm) CopyState {
	state := CopyState{Initialized: true}
	if term == nil {
		return state
	}

	start, _, total := term.VisibleLineRange()
	cursorLine := start
	cursorX := 0
	if term.ViewOffset == 0 {
		cursorLine = start + clamp(term.CursorY, 0, term.Height-1)
		cursorX = clamp(term.CursorX, 0, term.Width-1)
	}

	if total > 0 {
		cursorLine = clamp(cursorLine, 0, total-1)
	}
	cursorX = clamp(cursorX, 0, max(0, term.Width-1))

	state.CursorLine = cursorLine
	state.CursorX = cursorX
	state.AnchorLine = cursorLine
	state.AnchorX = cursorX
	state.Selecting = false
	SyncCopyState(term, &state)
	return state
}

// ToggleCopySelection starts or cancels selection anchored at the cursor.
func ToggleCopySelection(state *CopyState) {
	if state == nil {
		return
	}
	if state.Selecting {
		state.Selecting = false
		state.AnchorX = state.CursorX
		state.AnchorLine = state.CursorLine
		return
	}
	state.Selecting = true
	state.AnchorX = state.CursorX
	state.AnchorLine = state.CursorLine
}

// ToggleRectangle toggles rectangle selection mode.
func ToggleRectangle(state *CopyState) {
	if state == nil {
		return
	}
	state.Rectangular = !state.Rectangular
}

// SyncCopyState clamps, scrolls to keep cursor visible, and updates selection highlight.
func SyncCopyState(term *vterm.VTerm, state *CopyState) {
	if term == nil || state == nil || !state.Initialized {
		return
	}

	total := term.TotalLines()
	if total <= 0 {
		term.ClearSelection()
		return
	}

	state.CursorLine = clamp(state.CursorLine, 0, total-1)
	state.AnchorLine = clamp(state.AnchorLine, 0, total-1)
	state.CursorX = clamp(state.CursorX, 0, max(0, term.Width-1))
	state.AnchorX = clamp(state.AnchorX, 0, max(0, term.Width-1))

	ensureCursorVisible(term, state)
	applyCopySelection(term, state)
}

// CopySelectionText returns the selected text for the current copy state.
func CopySelectionText(term *vterm.VTerm, state *CopyState) string {
	if term == nil || state == nil || !state.Selecting {
		return ""
	}
	if state.Rectangular {
		return rectSelectionText(term, state)
	}
	return term.GetTextRange(state.AnchorX, state.AnchorLine, state.CursorX, state.CursorLine)
}

// MoveWordForward moves to the start of the next word.
func MoveWordForward(term *vterm.VTerm, state *CopyState) {
	if term == nil || state == nil || !state.Initialized {
		return
	}
	total := term.TotalLines()
	if total == 0 {
		return
	}

	line := clamp(state.CursorLine, 0, total-1)
	for {
		runes, runeX := lineRunes(term, line)
		idx := runeIndexAtX(runeX, state.CursorX)
		if idx < len(runes) && isWordRune(runes[idx]) {
			for idx < len(runes) && isWordRune(runes[idx]) {
				idx++
			}
		} else {
			for idx < len(runes) && !isWordRune(runes[idx]) {
				idx++
			}
		}

		for idx < len(runes) && !isWordRune(runes[idx]) {
			idx++
		}

		if idx < len(runes) {
			state.CursorLine = line
			state.CursorX = runeX[idx]
			SyncCopyState(term, state)
			return
		}
		line++
		if line >= total {
			state.CursorLine = total - 1
			state.CursorX = max(0, term.Width-1)
			SyncCopyState(term, state)
			return
		}
		state.CursorX = 0
	}
}

// MoveWordBackward moves to the start of the previous word.
func MoveWordBackward(term *vterm.VTerm, state *CopyState) {
	if term == nil || state == nil || !state.Initialized {
		return
	}
	total := term.TotalLines()
	if total == 0 {
		return
	}

	line := clamp(state.CursorLine, 0, total-1)
	idx := -1
	for line >= 0 {
		runes, runeX := lineRunes(term, line)
		if len(runes) == 0 {
			line--
			idx = -1
			continue
		}

		if idx < 0 {
			idx = runeIndexAtX(runeX, state.CursorX) - 1
			if idx >= len(runes) {
				idx = len(runes) - 1
			}
		}
		for idx >= 0 && !isWordRune(runes[idx]) {
			idx--
		}
		if idx >= 0 {
			for idx > 0 && isWordRune(runes[idx-1]) {
				idx--
			}
			state.CursorLine = line
			state.CursorX = runeX[idx]
			SyncCopyState(term, state)
			return
		}
		line--
		idx = -1
	}
}

// MoveWordEnd moves to the end of the current or next word.
func MoveWordEnd(term *vterm.VTerm, state *CopyState) {
	if term == nil || state == nil || !state.Initialized {
		return
	}
	total := term.TotalLines()
	if total == 0 {
		return
	}

	line := clamp(state.CursorLine, 0, total-1)
	for {
		runes, runeX := lineRunes(term, line)
		idx := runeIndexAtX(runeX, state.CursorX)
		if idx < 0 {
			idx = 0
		}
		if idx < len(runes) && !isWordRune(runes[idx]) {
			for idx < len(runes) && !isWordRune(runes[idx]) {
				idx++
			}
		}
		if idx < len(runes) && isWordRune(runes[idx]) {
			for idx+1 < len(runes) && isWordRune(runes[idx+1]) {
				idx++
			}
			state.CursorLine = line
			state.CursorX = runeX[idx]
			SyncCopyState(term, state)
			return
		}
		line++
		if line >= total {
			state.CursorLine = total - 1
			state.CursorX = max(0, term.Width-1)
			SyncCopyState(term, state)
			return
		}
		state.CursorX = 0
	}
}

func ensureCursorVisible(term *vterm.VTerm, state *CopyState) {
	start, end, total := term.VisibleLineRange()
	if total <= 0 || term.Height <= 0 {
		return
	}

	var newStart int
	switch {
	case state.CursorLine < start:
		newStart = state.CursorLine
	case state.CursorLine >= end:
		newStart = state.CursorLine - term.Height + 1
	default:
		return
	}

	if newStart < 0 {
		newStart = 0
	}
	maxStart := total - term.Height
	if maxStart < 0 {
		maxStart = 0
	}
	if newStart > maxStart {
		newStart = maxStart
	}

	newOffset := total - term.Height - newStart
	if newOffset < 0 {
		newOffset = 0
	}
	maxOffset := term.MaxViewOffset()
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	term.ViewOffset = newOffset
}

func applyCopySelection(term *vterm.VTerm, state *CopyState) {
	if term == nil || state == nil {
		return
	}

	startX, startLine := state.CursorX, state.CursorLine
	endX, endLine := state.CursorX, state.CursorLine
	if state.Selecting {
		startX, startLine = state.AnchorX, state.AnchorLine
		endX, endLine = state.CursorX, state.CursorLine
	}

	if startLine > endLine {
		startLine, endLine = endLine, startLine
		if !state.Rectangular {
			startX, endX = endX, startX
		}
	}
	if state.Rectangular && startX > endX {
		startX, endX = endX, startX
	} else if !state.Rectangular && startLine == endLine && startX > endX {
		startX, endX = endX, startX
	}

	viewStart, viewEnd, _ := term.VisibleLineRange()
	if endLine < viewStart || startLine >= viewEnd {
		term.ClearSelection()
		return
	}

	visibleStartLine := startLine
	visibleEndLine := endLine
	visibleStartX := startX
	visibleEndX := endX

	if visibleStartLine < viewStart {
		visibleStartLine = viewStart
		if !state.Rectangular {
			visibleStartX = 0
		}
	}
	if visibleEndLine >= viewEnd {
		visibleEndLine = viewEnd - 1
		if !state.Rectangular {
			visibleEndX = max(0, term.Width-1)
		}
	}

	startY := visibleStartLine - viewStart
	endY := visibleEndLine - viewStart
	term.SetSelection(visibleStartX, startY, visibleEndX, endY, true, state.Rectangular)
}
