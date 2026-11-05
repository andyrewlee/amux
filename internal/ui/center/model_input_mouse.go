package center

import (
	"fmt"
	"strings"

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
//
// Routing depends on the pane's screen state, which only tmux knows (it
// absorbs the pane's alternate-screen transitions rather than relaying
// them). A fresh cached probe decides inline; a stale one defers the whole
// decision to the tab actor (tabEventWheel), which probes once and routes —
// one tmux round-trip per freshness window, never on the UI goroutine.
func (m *Model) updateMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	tab := m.activeMouseTab()
	if tab == nil {
		return m, nil
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}

	delta := 0
	withMu(&tab.mu, func() {
		if tab.Terminal != nil {
			delta = common.ScrollDeltaForHeight(tab.Terminal.Height, 8)
		}
	})
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

	termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)
	// Shift+wheel is the standard force-local-scroll override (iTerm2, kitty,
	// xterm): it bypasses forwarding so the user can always scroll amux's own
	// scrollback, even when the hosted app wants the wheel. On a pane that
	// owns its viewport there is no local history, so it is a no-op there by
	// construction.
	if !msg.Mod.Contains(tea.ModShift) && inBounds {
		forwarded := false
		stale := false
		input := ""
		withMu(&tab.mu, func() {
			if tab.Terminal == nil {
				return
			}
			// Mouse reporting is relayed by tmux, so it is always current;
			// the pane's screen flag is only as fresh as the last probe.
			if tab.Terminal.MouseReportingEnabled() || !tab.paneStateIsStaleLocked() {
				input = mouseWheelInputSequence(tab.Terminal, msg.Button, termX, termY)
				return
			}
			stale = true
		})
		switch {
		case stale:
			m.dispatchOrHandleTabEvent(tabEvent{
				tab:             tab,
				workspaceID:     m.workspaceID(),
				tabID:           tab.ID,
				kind:            tabEventWheel,
				termX:           termX,
				termY:           termY,
				inBounds:        true,
				mouseButton:     msg.Button,
				delta:           delta,
				wheelAllowProbe: true,
			})
			return m, nil
		case input != "":
			forwarded = true
			m.dispatchOrHandleTabEvent(tabEvent{
				tab:         tab,
				workspaceID: m.workspaceID(),
				tabID:       tab.ID,
				kind:        tabEventSendMouse,
				input:       []byte(input),
			})
		}
		if forwarded {
			return m, nil
		}
	}

	// Pane-primary screens keep real local scrollback, fed by the scroll
	// events tmux relays; pane-alt applications own their viewport and have
	// nothing here to scroll.
	if withMuVal(&tab.mu, func() bool {
		return tab.Terminal != nil && tab.Terminal.PaneAlternateScreen
	}) {
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

func mouseWheelInputSequence(term *vterm.VTerm, button tea.MouseButton, termX, termY int) string {
	if term == nil || termX < 0 || termY < 0 {
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
	if term.MouseReportingEnabled() {
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
	if term.WheelScrollBecomesCursorKeys() {
		return wheelArrowKeySequence(term, button)
	}
	return ""
}

// altScrollWheelLines is how many cursor-key presses one wheel notch becomes
// under DECSET 1007 alternate scroll — the common modern terminal default
// (kitty, foot, WezTerm).
const altScrollWheelLines = 3

// wheelArrowKeySequence encodes a wheel event as cursor-key presses for an
// alternate-screen application that enabled DECSET 1007 instead of mouse
// reporting. DECCKM decides SS3 vs CSI encoding, exactly as a keypress would.
func wheelArrowKeySequence(term *vterm.VTerm, button tea.MouseButton) string {
	up := "\x1b[A"
	down := "\x1b[B"
	if term.ApplicationCursorKeys() {
		up = "\x1bOA"
		down = "\x1bOB"
	}
	seq := up
	if button == tea.MouseWheelDown {
		seq = down
	}
	return strings.Repeat(seq, altScrollWheelLines)
}

func (m *Model) getDiffViewer(tab *Tab) *diff.Model {
	if tab == nil {
		return nil
	}
	return withMuVal(&tab.mu, func() *diff.Model {
		return tab.DiffViewer
	})
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
