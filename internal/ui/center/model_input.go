package center

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

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
			// A diff viewer's `/` query field owns paste while editing —
			// the PTY path must not touch it.
			tab.mu.Lock()
			dv := tab.DiffViewer
			searching := dv != nil && dv.Searching()
			tab.mu.Unlock()
			if searching {
				if handled, cmd := m.dispatchDiffInput(tab, msg); handled {
					return m, cmd
				}
				return m, nil
			}
			// Admission is synchronous and ordered; the writer stamps
			// local-input timing after the PTY write actually happens so queue
			// latency does not look like local echo.
			payload := ansi.BracketedPasteStart + msg.Content + ansi.BracketedPasteEnd
			res, gen := m.admitTabInput(tab, payload, "Paste", true)
			if res == tabInputNoTerminal {
				return m, nil
			}
			logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
			cmds = append(cmds, m.rejectedTabInputCmd(tab, res, gen), m.userInputActivityTagCmd(tab))
			return m, common.SafeBatch(cmds...)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.updateKeyPress(msg)

	case messages.LaunchAgent:
		return m.updateLaunchAgent(msg)

	case messages.OpenFileInVim:
		return m.updateOpenFileInVim(msg)

	case messages.AttachRunSession:
		return m, m.CreateRunViewerTab(msg.Workspace, msg.SessionName)

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

	case messages.WorkspaceShelved:
		return m.updateWorkspaceShelved(msg)

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

	case activityTagFlushDue:
		m.handleActivityTagFlushDue()

	case selectionScrollTick:
		cmd := m.updateSelectionScrollTick(msg)
		cmds = append(cmds, cmd)

	case diffResultMsg:
		// An async diff-load result addressed to the tab that issued it.
		tab, _ := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "diff result")
		if tab == nil {
			break // tab closed mid-load — nothing to deliver to
		}
		if handled, cmd := m.dispatchDiffInput(tab, msg.Inner); handled && cmd != nil {
			cmds = append(cmds, cmd)
		}

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
