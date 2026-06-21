package center

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (m *Model) updateKeyPress(msg tea.KeyPressMsg) (*Model, tea.Cmd) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	logging.Debug("Center received key: %s, focused=%v, hasTabs=%v, numTabs=%d",
		msg.String(), m.focused, m.hasActiveAgent(), len(tabs))

	// Cmd+C copies the current selection without forwarding or clearing it.
	if model, cmd, handled := m.handleCopyKey(msg, tabs, activeIdx); handled {
		return model, cmd
	}
	// Any other key clears an active selection.
	m.clearSelectionOnType(tabs, activeIdx)

	if !m.focused {
		logging.Debug("Center not focused, ignoring key")
		return m, nil
	}
	if !m.hasActiveAgent() {
		return m, nil
	}
	return m.forwardKeyToActiveTab(msg, tabs[activeIdx])
}

func (m *Model) handleCopyKey(msg tea.KeyPressMsg, tabs []*Tab, activeIdx int) (*Model, tea.Cmd, bool) {
	k := msg.Key()
	isCopyKey := k.Mod.Contains(tea.ModSuper) && k.Code == 'c'
	if !isCopyKey || len(tabs) == 0 || activeIdx >= len(tabs) {
		return m, nil, false
	}
	tab := tabs[activeIdx]
	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionCopy,
			notifyCopy:  true,
		}) {
			return m, nil, true
		}
	}
	tab.mu.Lock()
	text := ""
	if tab.Terminal != nil && tab.Terminal.HasSelection() {
		text = tab.Terminal.SelectedText()
	}
	tab.mu.Unlock()
	common.CopyToClipboardWithLog(text, "Cmd+C clipboard")
	return m, nil, true
}

func (m *Model) clearSelectionOnType(tabs []*Tab, activeIdx int) {
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	sent := false
	if m.isTabActorReady() {
		sent = m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSelectionClear,
		})
	}
	if !sent {
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ClearSelection()
		}
		tab.Selection = common.SelectionState{}
		tab.selectionScroll.Reset()
		tab.mu.Unlock()
	}
}

func (m *Model) forwardKeyToActiveTab(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd) {
	tab.mu.Lock()
	dv := tab.DiffViewer
	tab.mu.Unlock()
	if dv != nil {
		return m.handleDiffViewerKey(msg, tab)
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return m, nil
	}
	return m.forwardKeyToTerminal(msg, tab)
}

func (m *Model) handleDiffViewerKey(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd) {
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))) {
		return m, m.closeCurrentTab()
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))) {
		before := m.getActiveTabIdx()
		m.nextTab()
		return m, m.tabSelectionChangedCmd(m.getActiveTabIdx() != before)
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))) {
		before := m.getActiveTabIdx()
		m.prevTab()
		return m, m.tabSelectionChangedCmd(m.getActiveTabIdx() != before)
	}
	if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
		return m, cmd
	}
	return m, nil
}

func (m *Model) forwardKeyToTerminal(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd) {
	if model, cmd, handled := m.handleTerminalCtrlKey(msg, tab); handled {
		return model, cmd
	}
	if model, cmd, handled := m.handleScrollbackKey(msg, tab); handled {
		return model, cmd
	}
	// Any typing returns a scrolled view to the live bottom before forwarding.
	m.scrollToBottomOnType(tab)
	return m.sendKeyToTerminal(msg, tab)
}

func (m *Model) handleTerminalCtrlKey(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		// Route Ctrl-C through the per-agent interrupt so agents that need more
		// than one Ctrl-C (e.g. Claude: InterruptCount 2, 200ms apart) are
		// actually interrupted. A plain key-forward only ever sends one 0x03.
		// Preserve the raw-key path's snap-to-bottom side effect.
		m.scrollToBottomOnType(tab)
		return m, m.interruptActiveAgentCmd(tab), true
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))):
		before := m.getActiveTabIdx()
		m.nextTab()
		return m, m.tabSelectionChangedCmd(m.getActiveTabIdx() != before), true
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))):
		before := m.getActiveTabIdx()
		m.prevTab()
		return m, m.tabSelectionChangedCmd(m.getActiveTabIdx() != before), true
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
		return m, m.closeCurrentTab(), true
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+]"))):
		// Escape hatch that won't conflict with embedded TUIs.
		before := m.getActiveTabIdx()
		m.nextTab()
		return m, m.tabSelectionChangedCmd(m.getActiveTabIdx() != before), true
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))):
		// This is Escape - let it go to terminal.
		stamp, halt := m.directSendStamped(tab, "\x1b", "Escape key")
		if halt {
			return m, stamp, true
		}
		return m, common.SafeBatch(stamp, m.userInputActivityTagCmd(tab)), true
	}
	return m, nil, false
}

// interruptActiveAgentCmd sends the agent's configured interrupt sequence
// (count + inter-press delay) off the Bubble Tea update loop, since the delay
// between presses must not block rendering. It also re-tags user-input activity
// so the working indicator updates promptly.
func (m *Model) interruptActiveAgentCmd(tab *Tab) tea.Cmd {
	agent := tab.Agent
	if agent == nil || m.agentManager == nil {
		return nil
	}
	interrupt := func() tea.Msg {
		_ = m.agentManager.SendInterrupt(agent)
		return nil
	}
	// Mirror the raw-key path's bookkeeping for the 0x03 it would have sent:
	// record the local-input echo window and re-tag user-input activity.
	return common.SafeBatch(
		interrupt,
		m.noteLocalInput(tab, m.workspaceID(), "\x03", time.Now()),
		m.userInputActivityTagCmd(tab),
	)
}

func (m *Model) handleScrollbackKey(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd, bool) {
	switch {
	case msg.Key().Code == tea.KeyPgUp:
		return m.scrollTerminalPage(tab, 1), nil, true
	case msg.Key().Code == tea.KeyPgDown:
		return m.scrollTerminalPage(tab, -1), nil, true
	}
	return m, nil, false
}

func (m *Model) scrollTerminalPage(tab *Tab, scrollPage int) *Model {
	if tab == nil || scrollPage == 0 {
		return m
	}
	if m.isTabActorReady() && m.sendTabEvent(tabEvent{
		tab:         tab,
		workspaceID: m.workspaceID(),
		tabID:       tab.ID,
		kind:        tabEventScrollPage,
		scrollPage:  scrollPage,
	}) {
		return m
	}
	tab.mu.Lock()
	if tab.Terminal != nil {
		delta := common.ScrollDeltaForHeight(tab.Terminal.Height, 4)
		m.scrollTerminalViewLocked(tab, delta*scrollPage)
	}
	tab.mu.Unlock()
	return m
}

func (m *Model) scrollToBottomOnType(tab *Tab) {
	sent := false
	if m.isTabActorReady() {
		sent = m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventScrollToBottom,
		})
	}
	if !sent {
		tab.mu.Lock()
		if tab.Terminal != nil && tab.Terminal.IsScrolled() {
			m.scrollTerminalToBottomLocked(tab)
		}
		tab.mu.Unlock()
	}
}

func (m *Model) sendKeyToTerminal(msg tea.KeyPressMsg, tab *Tab) (*Model, tea.Cmd) {
	input := common.KeyToBytes(msg)
	if len(input) == 0 {
		logging.Debug("keyToBytes returned empty for: %s", msg.String())
		return m, nil
	}
	logging.Debug("Sending to terminal: %q (len=%d)", input, len(input))

	var cmds []tea.Cmd
	queued := false
	if m.isTabActorReady() {
		queued = m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: m.workspaceID(),
			tabID:       tab.ID,
			kind:        tabEventSendInput,
			input:       input,
		})
	}
	// The actor-queued path stamps local-input timing after the PTY write; only
	// the direct-send fallback stamps here.
	if !queued {
		stamp, halt := m.directSendStamped(tab, string(input), "Direct input")
		if halt {
			return m, stamp
		}
		cmds = append(cmds, stamp)
	}
	cmds = append(cmds, m.userInputActivityTagCmd(tab))
	return m, common.SafeBatch(cmds...)
}

// directSendStamped sends data straight to the terminal. It returns halt=true
// when the caller should return (m, cmd) immediately — cmd is the error command,
// or nil when the send was a no-op. On a successful send it returns
// (noteLocalInputCmd, false): the local-input echo window is recorded here
// because the direct path bypasses the actor. The actor-queued path must NOT
// call this (it stamps after its own PTY write).
func (m *Model) directSendStamped(tab *Tab, data, label string) (cmd tea.Cmd, halt bool) {
	_, sent, cmd := m.directSendToTerminal(tab, data, label)
	if cmd != nil {
		return cmd, true
	}
	if !sent {
		return nil, true
	}
	return m.noteLocalInput(tab, m.workspaceID(), data, time.Now()), false
}
