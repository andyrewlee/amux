package center

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
	"github.com/andyrewlee/amux/internal/vterm"
)

// Mouse gestures route through the tab actor via dispatchOrHandleTabEvent:
// there is exactly one implementation of each gesture (the handlers in
// tab_actor_selection.go / tab_actor.go), whether the actor accepts the event
// or the fallback runs it synchronously. Redraws and follow-up commands
// (selection-scroll ticks, clipboard results) come back through msgSink.

// activeMouseTab returns the tab that should receive mouse input, or nil when
// the pane is unfocused, has no active agent, or the index is stale.
func (m *Model) activeMouseTab() *Tab {
	if !m.focused || !m.hasActiveAgent() {
		return nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return nil
	}
	return tabs[activeIdx]
}

// updateMouseClick handles tea.MouseClickMsg in the Update switch.
func (m *Model) updateMouseClick(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	// Handle tab bar clicks (e.g., the plus button) even without an active agent.
	if msg.Button == tea.MouseLeft {
		if cmd := m.handleTabBarClick(msg); cmd != nil {
			return m, cmd
		}
	}

	tab := m.activeMouseTab()
	if tab == nil {
		return m, nil
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)
	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventSelectionStart,
		termX:       termX,
		termY:       termY,
		inBounds:    inBounds,
	})
	return m, nil
}

// updateMouseMotion handles tea.MouseMotionMsg in the Update switch.
func (m *Model) updateMouseMotion(msg tea.MouseMotionMsg) (*Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tab := m.activeMouseTab()
	if tab == nil {
		return m, nil
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)
	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventSelectionUpdate,
		termX:       termX,
		termY:       termY,
	})
	return m, nil
}

// updateMouseRelease handles tea.MouseReleaseMsg in the Update switch.
func (m *Model) updateMouseRelease(msg tea.MouseReleaseMsg) (*Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	tab := m.activeMouseTab()
	if tab == nil {
		return m, nil
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventSelectionFinish,
	})
	return m, nil
}

// updateMouseWheel handles tea.MouseWheelMsg in the Update switch.
func (m *Model) updateMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	tab := m.activeMouseTab()
	if tab == nil {
		return m, nil
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}
	if m.forwardMouseWheelToTerminal(msg, tab) {
		return m, nil
	}

	delta := 0
	tab.mu.Lock()
	if tab.Terminal != nil {
		delta = common.ScrollDeltaForHeight(tab.Terminal.Height, 8)
	}
	tab.mu.Unlock()
	if delta == 0 {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
	case tea.MouseWheelDown:
		delta = -delta
	default:
		return m, nil
	}
	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventScrollBy,
		delta:       delta,
	})
	return m, nil
}

// forwardMouseWheelToTerminal forwards a wheel event to the hosted terminal
// when the agent has mouse reporting enabled and the pointer is inside the
// content area. Returns true when the event was consumed.
func (m *Model) forwardMouseWheelToTerminal(msg tea.MouseWheelMsg, tab *Tab) bool {
	if tab == nil {
		return false
	}
	termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)
	if !inBounds {
		return false
	}

	input := ""
	tab.mu.Lock()
	if tab.Terminal != nil {
		input = mouseWheelInputSequence(tab.Terminal, msg.Button, termX, termY)
	}
	tab.mu.Unlock()
	if input == "" {
		return false
	}

	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventSendMouse,
		input:       []byte(input),
	})
	return true
}

func mouseWheelInputSequence(term *vterm.VTerm, button tea.MouseButton, termX, termY int) string {
	if term == nil || !term.MouseReportingEnabled() || termX < 0 || termY < 0 {
		return ""
	}
	buttonCode := 0
	switch button {
	case tea.MouseWheelUp:
		buttonCode = 64
	case tea.MouseWheelDown:
		buttonCode = 65
	default:
		return ""
	}
	x := termX + 1
	y := termY + 1
	if term.MouseSGRMode() {
		return fmt.Sprintf("\x1b[<%d;%d;%dM", buttonCode, x, y)
	}
	if x > 223 || y > 223 {
		return ""
	}
	return string([]byte{0x1b, '[', 'M', byte(buttonCode + 32), byte(x + 32), byte(y + 32)})
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
	ev := tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventDiffInput,
		diffMsg:     msg,
	}
	if m.isTabActorReady() && m.sendTabEvent(ev) {
		return true, nil
	}
	return true, m.updateDiffViewer(tab, msg)
}

// updateSelectionScrollTick handles selectionScrollTick.
func (m *Model) updateSelectionScrollTick(msg selectionScrollTick) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	m.dispatchOrHandleTabEvent(tabEvent{
		tab:         tab,
		workspaceID: msg.WorkspaceID,
		tabID:       msg.TabID,
		kind:        tabEventSelectionScrollTick,
		gen:         msg.Gen,
		seq:         msg.Seq,
	})
	return nil
}
