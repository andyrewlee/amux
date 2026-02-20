package center

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

const activityTagThrottle = 1 * time.Second

func (m *Model) userInputActivityTagCmd(tab *Tab) tea.Cmd {
	if tab == nil || tab.isClosed() || !m.isChatTab(tab) {
		return nil
	}
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if sessionName == "" {
		return nil
	}
	now := time.Now()
	if now.Sub(tab.lastInputTagAt) < activityTagThrottle {
		return nil
	}
	tab.lastInputTagAt = now
	opts := m.getTmuxOptions()
	timestamp := now.UnixMilli()
	return func() tea.Msg {
		_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastInputAt, strconv.FormatInt(timestamp, 10), opts)
		return nil
	}
}

// updateLaunchAgent handles messages.LaunchAgent.
func (m *Model) updateLaunchAgent(msg messages.LaunchAgent) (*Model, tea.Cmd) {
	return m, m.createAgentTab(msg.Assistant, msg.Workspace)
}

// updateOpenFileInVim handles messages.OpenFileInVim.
func (m *Model) updateOpenFileInVim(msg messages.OpenFileInVim) (*Model, tea.Cmd) {
	return m, m.createVimTab(msg.Path, msg.Workspace)
}

// updatePtyTabCreateResult handles ptyTabCreateResult.
func (m *Model) updatePtyTabCreateResult(msg ptyTabCreateResult) (*Model, tea.Cmd) {
	return m, m.handlePtyTabCreated(msg)
}

// updatePtyTabReattachResult handles ptyTabReattachResult.
func (m *Model) updatePtyTabReattachResult(msg ptyTabReattachResult) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || msg.Agent == nil {
		return m, nil
	}
	rows := msg.Rows
	cols := msg.Cols
	if rows <= 0 || cols <= 0 {
		tm := m.terminalMetrics()
		rows = tm.Height
		cols = tm.Width
	}
	tab.mu.Lock()
	createdTerminal := false
	if tab.Terminal == nil {
		tab.Terminal = vterm.New(cols, rows)
		createdTerminal = true
	}
	if tab.Terminal != nil {
		tab.Terminal.AllowAltScreenScrollback = true
		if createdTerminal || len(tab.Terminal.Scrollback) == 0 {
			tab.Terminal.PrependScrollback(msg.ScrollbackCapture)
		}
	}
	tab.Agent = msg.Agent
	tab.SessionName = msg.Agent.Session
	tab.activityANSIState = ansiActivityText
	tab.Detached = false
	tab.reattachInFlight = false
	tab.Running = true
	tab.mu.Unlock()

	if tab.Terminal != nil && msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := msg.WorkspaceID
		tabID := tab.ID
		tab.Terminal.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if err := agentTerm.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				if m.msgSink != nil {
					m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
				}
			}
		})
	}

	m.resizePTY(tab, rows, cols)

	cmd := m.startPTYReader(msg.WorkspaceID, tab)
	return m, common.SafeBatch(cmd, func() tea.Msg {
		return messages.TabReattached{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	})
}

// updatePtyTabReattachFailed handles ptyTabReattachFailed.
func (m *Model) updatePtyTabReattachFailed(msg ptyTabReattachFailed) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return m, nil
	}
	tab.mu.Lock()
	tab.Running = false
	tab.reattachInFlight = false
	// Any stopped reattach clears Detached so the tab shows as stopped.
	if msg.Stopped {
		tab.Detached = false
	}
	tab.mu.Unlock()
	logging.Warn("Reattach failed for tab %s: %v", msg.TabID, msg.Err)
	action := msg.Action
	if action == "" {
		action = "reattach"
	}
	label := "Reattach"
	switch action {
	case "restart":
		label = "Restart"
	case "reattach":
		label = "Reattach"
	}
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	}, func() tea.Msg {
		return messages.Toast{
			Message: fmt.Sprintf("%s failed: %v", label, msg.Err),
			Level:   messages.ToastWarning,
		}
	})
}

// updateTabSessionStatus handles messages.TabSessionStatus.
func (m *Model) updateTabSessionStatus(msg messages.TabSessionStatus) (*Model, tea.Cmd) {
	if msg.Status != "stopped" {
		return m, nil
	}
	tab := m.getTabBySession(msg.WorkspaceID, msg.SessionName)
	if tab == nil {
		return m, nil
	}
	m.stopPTYReader(tab)
	tab.mu.Lock()
	agent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if agent != nil {
		_ = m.agentManager.CloseAgent(agent)
	}
	tab.mu.Lock()
	tab.Running = false
	tab.Detached = false
	tab.activityANSIState = ansiActivityText
	tab.mu.Unlock()
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(tab.ID)}
	})
}

// updateTabActorReady handles tabActorReady.
func (m *Model) updateTabActorReady(_ tabActorReady) (*Model, tea.Cmd) {
	m.setTabActorReady()
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateTabActorHeartbeat handles tabActorHeartbeat.
func (m *Model) updateTabActorHeartbeat(_ tabActorHeartbeat) (*Model, tea.Cmd) {
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateOpenDiff handles messages.OpenDiff.
func (m *Model) updateOpenDiff(msg messages.OpenDiff) (*Model, tea.Cmd) {
	if msg.Change == nil {
		return m, nil
	}
	return m, m.createDiffTab(msg.Change, msg.Mode, msg.Workspace)
}

// updateWorkspaceDeleted handles messages.WorkspaceDeleted.
func (m *Model) updateWorkspaceDeleted(msg messages.WorkspaceDeleted) (*Model, tea.Cmd) {
	m.CleanupWorkspace(msg.Workspace)
	return m, nil
}

// updateTabSelectionResult handles tabSelectionResult.
func (m *Model) updateTabSelectionResult(msg tabSelectionResult) (*Model, tea.Cmd) {
	if msg.clipboard != "" {
		if err := common.CopyToClipboard(msg.clipboard); err != nil {
			logging.Error("Failed to copy to clipboard: %v", err)
		} else {
			logging.Info("Copied %d chars to clipboard", len(msg.clipboard))
		}
	}
	return m, nil
}

// updateSelectionTickRequest handles selectionTickRequest.
func (m *Model) updateSelectionTickRequest(msg selectionTickRequest) (*Model, tea.Cmd) {
	cmd := common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: msg.workspaceID, TabID: msg.tabID, Gen: msg.gen}
	})
	return m, cmd
}

// updateTabDiffCmd handles tabDiffCmd.
func (m *Model) updateTabDiffCmd(msg tabDiffCmd) (*Model, tea.Cmd) {
	return m, msg.cmd
}
