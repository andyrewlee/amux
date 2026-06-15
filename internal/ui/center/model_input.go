package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// directSendToTerminal sends data directly to the terminal, handling errors.
// Returns whether data was actually sent and an optional command for failures.
func (m *Model) directSendToTerminal(tab *Tab, data, label string) (*Model, bool, tea.Cmd) {
	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return m, false, nil
	}
	m.tracePTYInput(tab, []byte(data))
	if err := tab.Agent.Terminal.SendString(data); err != nil {
		logging.Warn("%s failed for tab %s: %v", label, tab.ID, err)
		tab.mu.Lock()
		tab.markDetachedLocked()
		tab.mu.Unlock()
		wsID := m.workspaceID()
		return m, false, func() tea.Msg {
			return TabInputFailed{TabID: tab.ID, WorkspaceID: wsID, Err: err}
		}
	}
	return m, true, nil
}

// noteLocalInput records local typing/editing activity for activity suppression
// and chat cursor tracking, and schedules a redraw for timer-driven cursor
// state changes.
func (m *Model) noteLocalInput(tab *Tab, workspaceID, data string, now time.Time) tea.Cmd {
	if tab == nil {
		return nil
	}
	recordLocalInputEchoWindow(tab, data, now)
	return m.scheduleChatCursorRefresh(tab, workspaceID, now)
}

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
				queued := m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: m.workspaceID(),
					tabID:       tab.ID,
					kind:        tabEventPaste,
					pasteText:   msg.Content,
				})
				// When the actor accepts the event, it will stamp local-input
				// timing after the PTY write actually happens. Doing it here at
				// enqueue time would make queue latency look like local echo.
				if !queued {
					if _, sent, cmd := m.directSendToTerminal(tab, "\x1b[200~"+msg.Content+"\x1b[201~", "Direct paste"); cmd != nil {
						return m, cmd
					} else if !sent {
						return m, nil
					}
					now := time.Now()
					payload := "\x1b[200~" + msg.Content + "\x1b[201~"
					cmds = append(cmds, m.noteLocalInput(tab, m.workspaceID(), payload, now))
				}
				logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
				cmds = append(cmds, m.userInputActivityTagCmd(tab))
				return m, common.SafeBatch(cmds...)
			}
			if _, sent, cmd := m.directSendToTerminal(tab, "\x1b[200~"+msg.Content+"\x1b[201~", "Direct paste"); cmd != nil {
				return m, cmd
			} else if !sent {
				return m, nil
			}
			logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
			now := time.Now()
			payload := "\x1b[200~" + msg.Content + "\x1b[201~"
			cmds = append(cmds, m.noteLocalInput(tab, m.workspaceID(), payload, now))
			cmds = append(cmds, m.userInputActivityTagCmd(tab))
			return m, common.SafeBatch(cmds...)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.updateKeyPress(msg)

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

	case tabActorRedraw:
		m.clearTabActorRedrawPending()
		return m, nil

	case PTYOutput:
		cmd := m.updatePTYOutput(msg)
		cmds = append(cmds, cmd)

	case PTYFlush:
		cmd := m.updatePTYFlush(msg)
		cmds = append(cmds, cmd)

	case PTYCursorRefresh:
		cmd := m.updatePTYCursorRefresh(msg)
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
