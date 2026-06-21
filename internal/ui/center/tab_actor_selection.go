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

	needTick, gen := common.DragSelect(
		tab.Terminal,
		&tab.Selection,
		&tab.selectionScroll,
		ev.termX, ev.termY, tab.Terminal.Width, tab.Terminal.Height,
		&tab.selectionLastTermX,
		func(delta int) { m.scrollTerminalViewLocked(tab, delta) },
		func(screenY int) int {
			absLine, _ := m.displayedScreenYToAbsoluteLineLocked(tab, screenY)
			return absLine
		},
	)

	if needTick && m.msgSink != nil {
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
	common.SelectionScrollTickStep(
		tab.Terminal,
		&tab.Selection,
		&tab.selectionScroll,
		tab.Terminal.Height,
		tab.selectionLastTermX,
		func(delta int) { m.scrollTerminalViewLocked(tab, delta) },
		func(screenY int) int {
			absLine, _ := m.displayedScreenYToAbsoluteLineLocked(tab, screenY)
			return absLine
		},
	)

	tab.mu.Unlock()
	if m.msgSink != nil {
		m.msgSink(selectionTickRequest{
			workspaceID: ev.workspaceID,
			tabID:       ev.tabID,
			gen:         ev.gen,
		})
	}
}
