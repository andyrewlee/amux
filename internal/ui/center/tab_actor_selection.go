package center

import "github.com/andyrewlee/amux/internal/ui/common"

func (m *Model) handleSelectionClear(ev tabEvent) {
	tab := ev.tab
	hadSelection := false
	tab.mu.Lock()
	if tab.Selection.Active {
		hadSelection = true
	}
	if tab.Terminal != nil {
		hadSelection = hadSelection || tab.Terminal.HasSelection()
		tab.Terminal.ClearSelection()
	}
	tab.Selection = common.SelectionState{}
	tab.selectionScroll.Reset()
	tab.mu.Unlock()
	if hadSelection {
		m.requestTabActorRedraw()
	}
}

func (m *Model) handleSelectionClearAndNotify(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	text := ""
	if ev.notifyCopy && tab.Terminal != nil && tab.Terminal.HasSelection() {
		text = tab.Terminal.SelectedText()
	}
	if tab.Terminal != nil {
		tab.Terminal.ClearSelection()
	}
	tab.Selection = common.SelectionState{}
	tab.selectionScroll.Reset()
	tab.mu.Unlock()
	if ev.notifyCopy && text != "" && m.msgSink != nil {
		m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
	}
}

func (m *Model) handleSelectionCopy(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	text := ""
	if ev.notifyCopy && tab.Terminal != nil && tab.Terminal.HasSelection() {
		text = tab.Terminal.SelectedText()
	}
	tab.mu.Unlock()
	if ev.notifyCopy && text != "" && m.msgSink != nil {
		m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
	}
}

func (m *Model) handleSelectionStart(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.Terminal.ClearSelection()
	}
	tab.Selection = common.SelectionState{}
	tab.selectionScroll.Reset()
	if ev.inBounds && tab.Terminal != nil {
		absLine, ok := m.displayedScreenYToAbsoluteLineLocked(tab, ev.termY)
		if !ok {
			tab.mu.Unlock()
			return
		}
		tab.Selection = common.SelectionState{
			Active:    true,
			StartX:    ev.termX,
			StartLine: absLine,
			EndX:      ev.termX,
			EndLine:   absLine,
		}
		tab.Terminal.SetSelection(ev.termX, absLine, ev.termX, absLine, true, false)
	}
	tab.mu.Unlock()
}

func (m *Model) handleSelectionUpdate(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if !tab.Selection.Active || tab.Terminal == nil {
		return
	}
	termWidth := tab.Terminal.Width
	termHeight := tab.Terminal.Height
	termX := ev.termX
	termY := ev.termY

	if termX < 0 {
		termX = 0
	}
	if termX >= termWidth {
		termX = termWidth - 1
	}

	// Set scroll direction from unclamped Y before clamping
	tab.selectionScroll.SetDirection(termY, termHeight)

	if termY < 0 {
		m.scrollTerminalViewLocked(tab, 1)
		termY = 0
	} else if termY >= termHeight {
		m.scrollTerminalViewLocked(tab, -1)
		termY = termHeight - 1
	}

	absLine, _ := m.displayedScreenYToAbsoluteLineLocked(tab, termY)
	common.ExtendSelection(tab.Terminal, &tab.Selection, termX, absLine)

	tab.selectionLastTermX = termX
	if needTick, gen := tab.selectionScroll.NeedsTick(); needTick && m.msgSink != nil {
		m.msgSink(selectionTickRequest{
			workspaceID: ev.workspaceID,
			tabID:       ev.tabID,
			gen:         gen,
		})
	}
}

func (m *Model) handleSelectionFinish(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if !tab.Selection.Active {
		tab.mu.Unlock()
		return
	}
	text := ""
	tab.Selection.Active = false
	tab.selectionScroll.Reset()
	if tab.Terminal != nil &&
		(tab.Selection.StartX != tab.Selection.EndX ||
			tab.Selection.StartLine != tab.Selection.EndLine) {
		text = tab.Terminal.SelectedText()
	}
	tab.mu.Unlock()
	if text != "" && m.msgSink != nil {
		m.msgSink(tabSelectionResult{workspaceID: ev.workspaceID, tabID: ev.tabID, clipboard: text})
	}
}

func (m *Model) handleSelectionScrollTick(ev tabEvent) {
	tab := ev.tab
	tab.mu.Lock()
	if !tab.Selection.Active || tab.Terminal == nil || !tab.selectionScroll.HandleTick(ev.gen) {
		tab.mu.Unlock()
		return
	}
	m.scrollTerminalViewLocked(tab, tab.selectionScroll.ScrollDir)

	// Update selection endpoint to viewport edge
	edgeY := 0
	if tab.selectionScroll.ScrollDir < 0 {
		edgeY = tab.Terminal.Height - 1
	}
	absLine, _ := m.displayedScreenYToAbsoluteLineLocked(tab, edgeY)
	endX := tab.selectionLastTermX
	common.ExtendSelection(tab.Terminal, &tab.Selection, endX, absLine)

	tab.mu.Unlock()
	if m.msgSink != nil {
		m.msgSink(selectionTickRequest{
			workspaceID: ev.workspaceID,
			tabID:       ev.tabID,
			gen:         ev.gen,
		})
	}
}
