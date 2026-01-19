package common

import (
	"github.com/andyrewlee/amux/internal/vterm"
)

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
