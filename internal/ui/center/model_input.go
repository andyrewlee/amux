package center

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	defer perf.Time("center_update")()
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return m.updateMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.updateMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.updateMouseRelease(msg)

	case tea.MouseWheelMsg:
		return m.updateMouseWheel(msg)

	case tea.PasteMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			if !m.focused {
				return m, nil
			}
			if m.isTabActorReady() {
				if !m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventPaste,
					pasteText:   msg.Content,
				}) {
					if tab.Agent != nil && tab.Agent.Terminal != nil {
						bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
						if err := tab.Agent.Terminal.SendString(bracketedText); err != nil {
							logging.Warn("Direct paste failed for tab %s: %v", tab.ID, err)
							tab.mu.Lock()
							tab.Running = false
							tab.Detached = true
							tab.mu.Unlock()
							return m, func() tea.Msg {
								return TabInputFailed{TabID: tab.ID, WorkspaceID: m.workspaceID(), Err: err}
							}
						}
					}
				}
				logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
				return m, m.userInputActivityTagCmd(tab)
			}
			if tab.Agent != nil && tab.Agent.Terminal != nil {
				bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
				if err := tab.Agent.Terminal.SendString(bracketedText); err != nil {
					logging.Warn("Direct paste failed for tab %s: %v", tab.ID, err)
					tab.mu.Lock()
					tab.Running = false
					tab.Detached = true
					tab.mu.Unlock()
					return m, func() tea.Msg {
						return TabInputFailed{TabID: tab.ID, WorkspaceID: m.workspaceID(), Err: err}
					}
				}
				logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
				return m, m.userInputActivityTagCmd(tab)
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		logging.Debug("Center received key: %s, focused=%v, hasTabs=%v, numTabs=%d",
			msg.String(), m.focused, m.hasActiveAgent(), len(tabs))

		// Check if this is Cmd+C (copy command)
		k := msg.Key()
		isCopyKey := k.Mod.Contains(tea.ModSuper) && k.Code == 'c'

		// Handle explicit Cmd+C to copy current selection
		if isCopyKey && len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventSelectionCopy,
					notifyCopy:  true,
				}) {
					return m, nil
				}
			}
			tab.mu.Lock()
			if tab.Terminal != nil && tab.Terminal.HasSelection() {
				text := tab.Terminal.GetSelectedText(
					tab.Terminal.SelStartX(), tab.Terminal.SelStartLine(),
					tab.Terminal.SelEndX(), tab.Terminal.SelEndLine(),
				)
				if text != "" {
					if err := common.CopyToClipboard(text); err != nil {
						logging.Error("Failed to copy to clipboard: %v", err)
					} else {
						logging.Info("Cmd+C copied %d chars to clipboard", len(text))
					}
				}
			}
			tab.mu.Unlock()
			return m, nil // Don't forward to terminal, don't clear selection
		}

		// Clear any selection when user types (except Cmd+C which is handled above)
		if len(tabs) > 0 && activeIdx < len(tabs) {
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
				tab.Selection = SelectionState{}
				tab.selectionScroll.Reset()
				tab.mu.Unlock()
			}
		}

		if !m.focused {
			logging.Debug("Center not focused, ignoring key")
			return m, nil
		}

		// When we have an active agent, handle keys
		if m.hasActiveAgent() {
			tab := tabs[activeIdx]
			logging.Debug("Has active agent, Agent=%v, Terminal=%v", tab.Agent != nil, tab.Agent != nil && tab.Agent.Terminal != nil)

			// DiffViewer tabs: forward keys to diff viewer
			tab.mu.Lock()
			dv := tab.DiffViewer
			tab.mu.Unlock()
			if dv != nil {
				// Handle ctrl+w for closing tab
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))) {
					return m, m.closeCurrentTab()
				}
				// Handle ctrl+n/p for tab switching
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))) {
					m.nextTab()
					return m, m.tabSelectionChangedCmd()
				}
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))) {
					m.prevTab()
					return m, m.tabSelectionChangedCmd()
				}
				// Forward all other keys to diff viewer
				if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
					return m, cmd
				}
				return m, nil
			}

			if tab.Agent != nil && tab.Agent.Terminal != nil {
				// Only intercept these specific keys - everything else goes to terminal
				switch {
				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))):
					m.nextTab()
					return m, m.tabSelectionChangedCmd()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))):
					m.prevTab()
					return m, m.tabSelectionChangedCmd()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
					// Close tab
					return m, m.closeCurrentTab()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+]"))):
					// Switch to next tab (escape hatch that won't conflict)
					m.nextTab()
					return m, m.tabSelectionChangedCmd()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))):
					// This is Escape - let it go to terminal
					if err := tab.Agent.Terminal.SendString("\x1b"); err != nil {
						logging.Warn("Escape key failed for tab %s: %v", tab.ID, err)
						tab.mu.Lock()
						tab.Running = false
						tab.Detached = true
						tab.mu.Unlock()
						return m, func() tea.Msg {
							return TabInputFailed{TabID: tab.ID, WorkspaceID: m.workspaceID(), Err: err}
						}
					}
					return m, m.userInputActivityTagCmd(tab)
				}

				// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
				switch msg.Key().Code {
				case tea.KeyPgUp:
					if m.isTabActorReady() {
						if m.sendTabEvent(tabEvent{
							tab:         tab,
							workspaceID: m.workspaceID(),
							tabID:       tab.ID,
							kind:        tabEventScrollPage,
							scrollPage:  1,
						}) {
							return m, nil
						}
					}
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 4)
					}
					tab.mu.Unlock()
					return m, nil

				case tea.KeyPgDown:
					if m.isTabActorReady() {
						if m.sendTabEvent(tabEvent{
							tab:         tab,
							workspaceID: m.workspaceID(),
							tabID:       tab.ID,
							kind:        tabEventScrollPage,
							scrollPage:  -1,
						}) {
							return m, nil
						}
					}
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
					}
					tab.mu.Unlock()
					return m, nil
				}

				// If scrolled, any typing goes back to live and sends key
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
						tab.Terminal.ScrollViewToBottom()
					}
					tab.mu.Unlock()
				}

				// Forward ALL keys to terminal (no Ctrl interceptions)
				input := common.KeyToBytes(msg)
				if len(input) > 0 {
					logging.Debug("Sending to terminal: %q (len=%d)", input, len(input))
					if m.isTabActorReady() {
						if !m.sendTabEvent(tabEvent{
							tab:         tab,
							workspaceID: m.workspaceID(),
							tabID:       tab.ID,
							kind:        tabEventSendInput,
							input:       input,
						}) {
							if tab.Agent != nil && tab.Agent.Terminal != nil {
								if err := tab.Agent.Terminal.SendString(string(input)); err != nil {
									logging.Warn("Direct input failed for tab %s: %v", tab.ID, err)
									tab.mu.Lock()
									tab.Running = false
									tab.Detached = true
									tab.mu.Unlock()
									return m, func() tea.Msg {
										return TabInputFailed{TabID: tab.ID, WorkspaceID: m.workspaceID(), Err: err}
									}
								}
							}
						}
					} else {
						if err := tab.Agent.Terminal.SendString(string(input)); err != nil {
							logging.Warn("Direct input failed for tab %s: %v", tab.ID, err)
							tab.mu.Lock()
							tab.Running = false
							tab.Detached = true
							tab.mu.Unlock()
							return m, func() tea.Msg {
								return TabInputFailed{TabID: tab.ID, WorkspaceID: m.workspaceID(), Err: err}
							}
						}
					}
					return m, m.userInputActivityTagCmd(tab)
				} else {
					logging.Debug("keyToBytes returned empty for: %s", msg.String())
				}
				return m, nil
			}
		}

	case messages.LaunchAgent:
		return m.updateLaunchAgent(msg)

	case messages.OpenFileInVim:
		return m.updateOpenFileInVim(msg)

	case ptyTabCreateResult:
		return m.updatePtyTabCreateResult(msg)

	case ptyTabReattachResult:
		return m.updatePtyTabReattachResult(msg)

	case ptyTabReattachFailed:
		return m.updatePtyTabReattachFailed(msg)

	case messages.TabSessionStatus:
		return m.updateTabSessionStatus(msg)

	case tabActorReady:
		return m.updateTabActorReady(msg)

	case tabActorHeartbeat:
		return m.updateTabActorHeartbeat(msg)

	case messages.OpenDiff:
		return m.updateOpenDiff(msg)

	case messages.WorkspaceDeleted:
		return m.updateWorkspaceDeleted(msg)

	case tabSelectionResult:
		return m.updateTabSelectionResult(msg)

	case selectionTickRequest:
		return m.updateSelectionTickRequest(msg)

	case tabDiffCmd:
		return m.updateTabDiffCmd(msg)

	case PTYOutput:
		cmd := m.updatePTYOutput(msg)
		cmds = append(cmds, cmd)

	case PTYFlush:
		cmd := m.updatePTYFlush(msg)
		cmds = append(cmds, cmd)

	case PTYStopped:
		cmd := m.updatePTYStopped(msg)
		cmds = append(cmds, cmd)

	case PTYRestart:
		cmd := m.updatePTYRestart(msg)
		cmds = append(cmds, cmd)

	case selectionScrollTick:
		cmd := m.updateSelectionScrollTick(msg)
		cmds = append(cmds, cmd)

	default:
		// Forward unknown messages to active viewer if one exists
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	return m, common.SafeBatch(cmds...)
}
