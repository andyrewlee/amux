package common

import (
	"strings"
	"unicode"

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

// StartSearch begins a forward or backward search prompt.
func StartSearch(state *CopyState, backward bool) {
	if state == nil {
		return
	}
	state.SearchActive = true
	state.SearchBackward = backward
	state.SearchQuery = ""
}

// CancelSearch exits search prompt without changing state.
func CancelSearch(state *CopyState) {
	if state == nil {
		return
	}
	state.SearchActive = false
	state.SearchQuery = ""
}

// AppendSearchQuery appends text to the current search prompt.
func AppendSearchQuery(state *CopyState, text string) {
	if state == nil || text == "" {
		return
	}
	state.SearchQuery += text
}

// BackspaceSearchQuery removes the last rune from the current search prompt.
func BackspaceSearchQuery(state *CopyState) {
	if state == nil || state.SearchQuery == "" {
		return
	}
	runes := []rune(state.SearchQuery)
	if len(runes) == 0 {
		state.SearchQuery = ""
		return
	}
	state.SearchQuery = string(runes[:len(runes)-1])
}

// ExecuteSearch runs the current search prompt and moves the cursor.
func ExecuteSearch(term *vterm.VTerm, state *CopyState) bool {
	if term == nil || state == nil {
		return false
	}
	query := state.SearchQuery
	backward := state.SearchBackward
	state.SearchActive = false
	state.SearchQuery = ""
	if query == "" {
		return false
	}
	state.LastSearch = query
	state.LastSearchBackward = backward
	return searchMove(term, state, query, backward)
}

// RepeatSearch repeats the last search.
func RepeatSearch(term *vterm.VTerm, state *CopyState, backward bool) bool {
	if term == nil || state == nil || state.LastSearch == "" {
		return false
	}
	state.LastSearchBackward = backward
	return searchMove(term, state, state.LastSearch, backward)
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

	newStart := start
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

func searchMove(term *vterm.VTerm, state *CopyState, query string, backward bool) bool {
	if term == nil || state == nil || query == "" {
		return false
	}

	queryRunes := []rune(query)
	total := term.TotalLines()
	if total == 0 {
		return false
	}

	startLine := clamp(state.CursorLine, 0, total-1)
	startX := clamp(state.CursorX, 0, max(0, term.Width-1))

	if backward {
		if searchBackward(term, queryRunes, startLine, startX, state) {
			SyncCopyState(term, state)
			return true
		}
		return false
	}

	if searchForward(term, queryRunes, startLine, startX, state) {
		SyncCopyState(term, state)
		return true
	}
	return false
}

func rectSelectionText(term *vterm.VTerm, state *CopyState) string {
	if term == nil || state == nil {
		return ""
	}
	width := term.Width
	if width < 1 {
		width = 1
	}
	total := term.TotalLines()
	if total == 0 {
		return ""
	}

	startLine, endLine := state.AnchorLine, state.CursorLine
	startX, endX := state.AnchorX, state.CursorX
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	if startX > endX {
		startX, endX = endX, startX
	}

	startLine = clamp(startLine, 0, total-1)
	endLine = clamp(endLine, 0, total-1)
	startX = clamp(startX, 0, width-1)
	endX = clamp(endX, 0, width-1)

	var result strings.Builder
	for line := startLine; line <= endLine; line++ {
		row := term.LineCells(line)
		for x := startX; x <= endX; x++ {
			r := ' '
			if row != nil && x < len(row) {
				cell := row[x]
				if cell.Width == 0 {
					continue
				}
				r = cell.Rune
				if r == 0 {
					r = ' '
				}
			}
			result.WriteRune(r)
		}
		if line < endLine {
			result.WriteRune('\n')
		}
	}
	return result.String()
}

func searchForward(term *vterm.VTerm, query []rune, startLine, startX int, state *CopyState) bool {
	total := term.TotalLines()
	if total == 0 {
		return false
	}

	for pass := 0; pass < 2; pass++ {
		lineStart := startLine
		lineEnd := total
		if pass == 1 {
			lineStart = 0
			lineEnd = startLine + 1
		}

		for line := lineStart; line < lineEnd; line++ {
			runes, runeX := lineRunes(term, line)
			if len(runes) == 0 || len(runes) < len(query) {
				continue
			}

			startIdx := 0
			endLimit := len(runes) - len(query)
			if line == startLine {
				cursorIdx := runeIndexAtX(runeX, startX)
				if pass == 0 {
					startIdx = cursorIdx + 1
				} else {
					startIdx = 0
					endLimit = cursorIdx - 1
				}
			}
			if startIdx < 0 {
				startIdx = 0
			}
			if endLimit < startIdx {
				continue
			}

			if idx := findForward(runes, query, startIdx, endLimit); idx >= 0 {
				state.CursorLine = line
				state.CursorX = runeX[idx]
				return true
			}
		}
	}
	return false
}

func searchBackward(term *vterm.VTerm, query []rune, startLine, startX int, state *CopyState) bool {
	total := term.TotalLines()
	if total == 0 {
		return false
	}

	for pass := 0; pass < 2; pass++ {
		lineStart := startLine
		lineEnd := -1
		if pass == 1 {
			lineStart = total - 1
			lineEnd = startLine - 1
		}

		for line := lineStart; line > lineEnd; line-- {
			runes, runeX := lineRunes(term, line)
			if len(runes) == 0 || len(runes) < len(query) {
				continue
			}

			startIdx := len(runes) - len(query)
			if line == startLine {
				cursorIdx := runeIndexAtX(runeX, startX) - 1
				if pass == 0 {
					startIdx = cursorIdx
				}
			}

			if startIdx < 0 {
				continue
			}
			if startIdx > len(runes)-len(query) {
				startIdx = len(runes) - len(query)
			}

			if idx := findBackward(runes, query, startIdx); idx >= 0 {
				state.CursorLine = line
				state.CursorX = runeX[idx]
				return true
			}
		}
	}
	return false
}

func findForward(line, query []rune, start, limit int) int {
	if len(query) == 0 || len(line) < len(query) {
		return -1
	}
	if start < 0 {
		start = 0
	}
	if limit > len(line)-len(query) {
		limit = len(line) - len(query)
	}
	for i := start; i <= limit; i++ {
		match := true
		for j := 0; j < len(query); j++ {
			if line[i+j] != query[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func findBackward(line, query []rune, start int) int {
	if len(query) == 0 || len(line) < len(query) {
		return -1
	}
	if start > len(line)-len(query) {
		start = len(line) - len(query)
	}
	for i := start; i >= 0; i-- {
		match := true
		for j := 0; j < len(query); j++ {
			if line[i+j] != query[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lineRunes(term *vterm.VTerm, line int) ([]rune, []int) {
	if term == nil {
		return nil, nil
	}
	cells := term.LineCells(line)
	if cells == nil {
		cells = vterm.MakeBlankLine(max(1, term.Width))
	}
	runes := make([]rune, 0, len(cells))
	runeX := make([]int, 0, len(cells))
	for x, cell := range cells {
		if cell.Width == 0 {
			continue
		}
		r := cell.Rune
		if r == 0 {
			r = ' '
		}
		runes = append(runes, r)
		runeX = append(runeX, x)
	}
	return runes, runeX
}

func runeIndexAtX(runeX []int, x int) int {
	if len(runeX) == 0 {
		return 0
	}
	idx := 0
	for i, rx := range runeX {
		if rx >= x {
			return i
		}
		idx = i + 1
	}
	return idx
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
