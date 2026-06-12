package center

import (
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

func scrolledChatHistoryScrollbackLen(term *vterm.VTerm) int {
	if term == nil {
		return 0
	}
	_, scrollbackLen := term.RenderBuffers()
	if scrollbackLen > len(term.Scrollback) {
		scrollbackLen = len(term.Scrollback)
	}
	return scrollbackLen
}

func scrolledChatHistoryMaxViewOffset(term *vterm.VTerm) int {
	if term == nil || (term.AltScreen && !term.CaptureNormalScreenOnClear) {
		return 0
	}
	scrollbackLen := scrolledChatHistoryScrollbackLen(term)
	if scrollbackLen <= 0 {
		return 0
	}
	height := term.Height
	if height <= 0 {
		height = 1
	}
	maxOffset := scrollbackLen - height + 1
	if maxOffset < 1 {
		maxOffset = 1
	}
	return maxOffset
}

func scrolledChatHistoryEffectiveViewOffset(term *vterm.VTerm) int {
	if term == nil {
		return 0
	}
	offset := term.ViewOffset
	maxOffset := scrolledChatHistoryMaxViewOffset(term)
	if maxOffset > 0 && offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

func scrolledChatHistoryVisibleRange(term *vterm.VTerm, height int) (startLine, endLine int, ok bool) {
	if term == nil || term.ViewOffset <= 0 || (term.AltScreen && !term.CaptureNormalScreenOnClear) {
		return 0, 0, false
	}
	if height <= 0 {
		height = term.Height
	}
	if height <= 0 {
		return 0, 0, false
	}
	scrollbackLen := scrolledChatHistoryScrollbackLen(term)
	if scrollbackLen <= 0 {
		return 0, 0, false
	}

	viewOffset := scrolledChatHistoryEffectiveViewOffset(term)
	startLine = scrollbackLen - height - viewOffset + 1
	if startLine < 0 {
		startLine = 0
	}
	endLine = startLine + height - 1
	if maxEnd := scrollbackLen - 1; endLine > maxEnd {
		endLine = maxEnd
	}
	return startLine, endLine, true
}

func scrolledChatHistoryScreenYToAbsoluteLine(term *vterm.VTerm, screenY int) (int, bool) {
	if term == nil {
		return 0, false
	}
	startLine, endLine, ok := scrolledChatHistoryVisibleRange(term, term.Height)
	if !ok {
		return term.ScreenYToAbsoluteLine(screenY), true
	}
	if screenY < 0 {
		screenY = 0
	} else if screenY >= term.Height {
		screenY = term.Height - 1
	}
	visibleRows := endLine - startLine + 1
	if visibleRows <= 0 {
		return 0, false
	}
	absLine := startLine + screenY
	if absLine > endLine {
		return endLine, false
	}
	return absLine, true
}

func (m *Model) displayedScreenYToAbsoluteLineLocked(tab *Tab, screenY int) (int, bool) {
	if tab == nil || tab.Terminal == nil {
		return 0, false
	}
	if m != nil && m.isChatTabLocked(tab) {
		return scrolledChatHistoryScreenYToAbsoluteLine(tab.Terminal, screenY)
	}
	return tab.Terminal.ScreenYToAbsoluteLine(screenY), true
}

func (m *Model) clampScrolledChatHistoryViewOffsetLocked(tab *Tab) {
	if m == nil || tab == nil || tab.Terminal == nil || !m.isChatTabLocked(tab) {
		return
	}
	if tab.Terminal.ViewOffset <= 0 {
		return
	}
	maxOffset := scrolledChatHistoryMaxViewOffset(tab.Terminal)
	if maxOffset == 0 {
		tab.Terminal.ScrollViewToBottom()
		tab.Terminal.NoteSyncViewportInteraction()
		return
	}
	if tab.Terminal.ViewOffset > maxOffset {
		tab.Terminal.ScrollViewTo(maxOffset)
		tab.Terminal.NoteSyncViewportInteraction()
	}
}

func (m *Model) scrollTerminalViewLocked(tab *Tab, delta int) {
	if tab == nil || tab.Terminal == nil || delta == 0 {
		return
	}
	m.clampScrolledChatHistoryViewOffsetLocked(tab)
	tab.Terminal.ScrollView(delta)
	tab.Terminal.NoteSyncViewportInteraction()
	m.clampScrolledChatHistoryViewOffsetLocked(tab)
}

func (m *Model) scrollTerminalToBottomLocked(tab *Tab) {
	if tab == nil || tab.Terminal == nil {
		return
	}
	tab.Terminal.ScrollViewToBottom()
	tab.Terminal.NoteSyncViewportInteraction()
}

func (m *Model) scrollTerminalToTopLocked(tab *Tab) {
	if tab == nil || tab.Terminal == nil {
		return
	}
	if m != nil && m.isChatTabLocked(tab) {
		maxOffset := scrolledChatHistoryMaxViewOffset(tab.Terminal)
		if maxOffset > 0 {
			tab.Terminal.ScrollViewTo(maxOffset)
			tab.Terminal.NoteSyncViewportInteraction()
			return
		}
	}
	tab.Terminal.ScrollViewToTop()
	tab.Terminal.NoteSyncViewportInteraction()
}

func (m *Model) displayedScrollInfoLocked(tab *Tab) (offset, maxOffset int) {
	if tab == nil || tab.Terminal == nil {
		return 0, 0
	}
	if m != nil && m.isChatTabLocked(tab) && tab.Terminal.ViewOffset > 0 {
		maxOffset = scrolledChatHistoryMaxViewOffset(tab.Terminal)
		if maxOffset == 0 {
			return 0, 0
		}
		offset = scrolledChatHistoryEffectiveViewOffset(tab.Terminal)
		return offset, maxOffset
	}
	return tab.Terminal.GetScrollInfo()
}

// applyScrolledChatHistoryViewLocked replaces a scrolled chat snapshot with a
// history-only view so the live prompt/output region does not appear mixed into
// the middle of scrollback while the user is reading older content.
//
// Caller must hold tab.mu.
func applyScrolledChatHistoryViewLocked(term *vterm.VTerm, snap *compositor.VTermSnapshot) {
	if term == nil || snap == nil || snap.ViewOffset <= 0 || (term.AltScreen && !term.CaptureNormalScreenOnClear) {
		return
	}

	width := snap.Width
	height := snap.Height
	if width <= 0 || height <= 0 {
		return
	}

	startLine, _, ok := scrolledChatHistoryVisibleRange(term, height)
	if !ok {
		return
	}
	scrollbackLen := scrolledChatHistoryScrollbackLen(term)

	screen := make([][]vterm.Cell, height)
	for i := 0; i < height; i++ {
		line := vterm.MakeBlankLine(width)
		lineIdx := startLine + i
		if lineIdx >= 0 && lineIdx < scrollbackLen {
			copy(line, term.Scrollback[lineIdx])
		}
		screen[i] = line
	}
	snap.Screen = screen
	rebaseScrolledChatSelection(term, snap, startLine)
}

func rebaseScrolledChatSelection(term *vterm.VTerm, snap *compositor.VTermSnapshot, startLine int) {
	if term == nil || snap == nil || !snap.SelActive {
		return
	}

	width := snap.Width
	height := snap.Height
	scrollbackLen := scrolledChatHistoryScrollbackLen(term)
	if width <= 0 || height <= 0 || scrollbackLen <= 0 {
		snap.SelActive = false
		return
	}

	startAbs := term.SelStartLine()
	endAbs := term.SelEndLine()
	startX := term.SelStartX()
	endX := term.SelEndX()

	if startAbs > endAbs || (startAbs == endAbs && startX > endX) {
		startAbs, endAbs = endAbs, startAbs
		startX, endX = endX, startX
	}

	visibleEnd := startLine + height - 1
	if _, endLine, ok := scrolledChatHistoryVisibleRange(term, height); ok {
		visibleEnd = endLine
	}

	if endAbs < startLine || startAbs > visibleEnd {
		snap.SelActive = false
		return
	}

	if startAbs < startLine {
		snap.SelStartY = 0
		startX = 0
	} else {
		snap.SelStartY = startAbs - startLine
	}

	if endAbs > visibleEnd {
		snap.SelEndY = visibleEnd - startLine
		endX = width - 1
	} else {
		snap.SelEndY = endAbs - startLine
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
