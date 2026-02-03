package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

// updateMouseClick handles tea.MouseClickMsg in the Update switch.
func (m *Model) updateMouseClick(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle tab bar clicks (e.g., the plus button) even without an active agent.
	if msg.Button == tea.MouseLeft {
		if cmd := m.handleTabBarClick(msg); cmd != nil {
			return m, cmd
		}
	}

	// Handle mouse events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}

	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	// Convert screen coordinates to terminal coordinates
	termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionStart,
			termX:       termX,
			termY:       termY,
			inBounds:    inBounds,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.Terminal.ClearSelection()
	}
	tab.Selection = SelectionState{}
	tab.selectionScroll.Reset()
	if inBounds && tab.Terminal != nil {
		absLine := tab.Terminal.ScreenYToAbsoluteLine(termY)
		tab.Selection = SelectionState{
			Active:    true,
			StartX:    termX,
			StartLine: absLine,
			EndX:      termX,
			EndLine:   absLine,
		}
		tab.Terminal.SetSelection(termX, absLine, termX, absLine, true, false)
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

// updateMouseMotion handles tea.MouseMotionMsg in the Update switch.
func (m *Model) updateMouseMotion(msg tea.MouseMotionMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle mouse drag events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionUpdate,
			termX:       termX,
			termY:       termY,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Selection.Active && tab.Terminal != nil {
		termWidth := tab.Terminal.Width
		termHeight := tab.Terminal.Height
		if termX < 0 {
			termX = 0
		}
		if termX >= termWidth {
			termX = termWidth - 1
		}

		// Set scroll direction from unclamped Y before clamping
		tab.selectionScroll.SetDirection(termY, termHeight)

		if termY < 0 {
			tab.Terminal.ScrollView(1)
			termY = 0
		} else if termY >= termHeight {
			tab.Terminal.ScrollView(-1)
			termY = termHeight - 1
		}
		absLine := tab.Terminal.ScreenYToAbsoluteLine(termY)
		startX := tab.Terminal.SelStartX()
		startLine := tab.Terminal.SelStartLine()
		if !tab.Terminal.HasSelection() {
			startX = tab.Selection.StartX
			startLine = tab.Selection.StartLine
		}
		tab.Selection.EndX = termX
		tab.Selection.EndLine = absLine
		tab.Terminal.SetSelection(startX, startLine, termX, absLine, true, false)
		tab.Selection.StartX = startX
		tab.Selection.StartLine = startLine

		tab.selectionLastTermX = termX
		if needTick, gen := tab.selectionScroll.NeedsTick(); needTick {
			wsID := m.workspaceID()
			tabID := tab.ID
			cmds = append(cmds, common.SafeTick(common.SelectionScrollTickInterval, func(time.Time) tea.Msg {
				return selectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
			}))
		}
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

// updateMouseRelease handles tea.MouseReleaseMsg in the Update switch.
func (m *Model) updateMouseRelease(msg tea.MouseReleaseMsg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle mouse release events for text selection
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionFinish,
		}) {
			return m, common.SafeBatch(cmds...)
		}
	}
	tab.mu.Lock()
	if tab.Selection.Active {
		if tab.Terminal != nil &&
			(tab.Selection.StartX != tab.Selection.EndX ||
				tab.Selection.StartLine != tab.Selection.EndLine) {
			text := tab.Terminal.GetSelectedText(
				tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
				tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
			)
			if text != "" {
				if err := common.CopyToClipboard(text); err != nil {
					logging.Error("Failed to copy to clipboard: %v", err)
				} else {
					logging.Info("Copied %d chars to clipboard", len(text))
				}
			}
		}
		tab.Selection.Active = false
		tab.selectionScroll.Reset()
	}
	tab.mu.Unlock()
	return m, common.SafeBatch(cmds...)
}

// updateMouseWheel handles tea.MouseWheelMsg in the Update switch.
func (m *Model) updateMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	if !m.focused || !m.hasActiveAgent() {
		return m, nil
	}

	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return m, nil
	}
	tab := tabs[activeIdx]
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	delta := 0
	tab.mu.Lock()
	if tab.Terminal != nil {
		delta = common.ScrollDeltaForHeight(tab.Terminal.Height, 8)
	}
	tab.mu.Unlock()
	if delta > 0 {
		if m.isTabActorReady() {
			sent := false
			if msg.Button == tea.MouseWheelUp {
				sent = m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventScrollBy,
					delta:       delta,
				})
			} else if msg.Button == tea.MouseWheelDown {
				sent = m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventScrollBy,
					delta:       -delta,
				})
			}
			if sent {
				return m, nil
			}
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			if msg.Button == tea.MouseWheelUp {
				tab.Terminal.ScrollView(delta)
			} else if msg.Button == tea.MouseWheelDown {
				tab.Terminal.ScrollView(-delta)
			}
		}
		tab.mu.Unlock()
	}
	return m, nil
}

func (m *Model) getDiffViewer(tab *Tab) *diff.Model {
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	dv := tab.DiffViewer
	tab.mu.Unlock()
	return dv
}

func (m *Model) dispatchDiffInput(tab *Tab, msg tea.Msg) (bool, tea.Cmd) {
	if tab == nil {
		return false, nil
	}
	dv := m.getDiffViewer(tab)
	if dv == nil {
		return false, nil
	}
	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventDiffInput,
			diffMsg:     msg,
		}) {
			return true, nil
		}
	}
	newDV, cmd := dv.Update(msg)
	tab.mu.Lock()
	tab.DiffViewer = newDV
	tab.mu.Unlock()
	return true, cmd
}

// updateSelectionScrollTick handles selectionScrollTick.
func (m *Model) updateSelectionScrollTick(msg selectionScrollTick) tea.Cmd {
	var cmds []tea.Cmd
	if m.isTabActorReady() {
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab == nil {
			return nil
		}
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: msg.WorkspaceID,
			tabID:       msg.TabID,
			kind:        tabEventSelectionScrollTick,
			gen:         msg.Gen,
		}) {
			return nil
		}
	}
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	if !tab.Selection.Active || tab.Terminal == nil || !tab.selectionScroll.HandleTick(msg.Gen) {
		tab.mu.Unlock()
		return nil
	}
	tab.Terminal.ScrollView(tab.selectionScroll.ScrollDir)

	// Update selection endpoint to viewport edge
	edgeY := 0
	if tab.selectionScroll.ScrollDir < 0 {
		edgeY = tab.Terminal.Height - 1
	}
	absLine := tab.Terminal.ScreenYToAbsoluteLine(edgeY)
	endX := tab.selectionLastTermX
	startX := tab.Terminal.SelStartX()
	startLine := tab.Terminal.SelStartLine()
	if !tab.Terminal.HasSelection() {
		startX = tab.Selection.StartX
		startLine = tab.Selection.StartLine
	}
	tab.Selection.EndX = endX
	tab.Selection.EndLine = absLine
	tab.Terminal.SetSelection(startX, startLine, endX, absLine, true, false)
	tab.Selection.StartX = startX
	tab.Selection.StartLine = startLine

	tab.mu.Unlock()
	tabID := msg.TabID
	wtID := msg.WorkspaceID
	cmds = append(cmds, common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: wtID, TabID: tabID, Gen: msg.Gen}
	}))
	return common.SafeBatch(cmds...)
}
