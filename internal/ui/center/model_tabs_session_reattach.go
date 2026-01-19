package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// ReattachActiveTab reattaches to a detached tmux session.
func (m *Model) ReattachActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	tab.mu.Lock()
	detached := tab.Detached
	reattachInFlight := tab.reattachInFlight
	sessionName := tab.SessionName
	if detached && !reattachInFlight {
		tab.reattachInFlight = true
	}
	tab.mu.Unlock()
	if !detached {
		return nil
	}
	if reattachInFlight {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		tab.mu.Lock()
		tab.reattachInFlight = false
		tab.mu.Unlock()
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		tab.mu.Lock()
		tab.reattachInFlight = false
		tab.mu.Unlock()
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(tab.Workspace.ID()), string(tab.ID))
	}
	assistant := tab.Assistant
	ws := tab.Workspace
	tabID := tab.ID
	opts := m.getTmuxOptions()
	return func() tea.Msg {
		state, err := tmux.SessionStateFor(sessionName, opts)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		if !state.Exists || !state.HasLivePane {
			if state.Exists && !state.HasLivePane {
				_ = tmux.KillSession(sessionName, opts)
			}
			tags := tmux.SessionTags{
				WorkspaceID: string(ws.ID()),
				TabID:       string(tabID),
				Type:        "agent",
				Assistant:   assistant,
				CreatedAt:   time.Now().Unix(),
				InstanceID:  m.instanceID,
			}
			agent, err := m.agentProvider.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
			if err != nil {
				return ptyTabReattachFailed{
					WorkspaceID: string(ws.ID()),
					TabID:       tabID,
					Err:         err,
					Stopped:     true,
					Action:      "reattach",
				}
			}
			scrollback, _ := tmux.CapturePane(sessionName, opts)
			return ptyTabReattachResult{
				WorkspaceID:       string(ws.ID()),
				TabID:             tabID,
				Agent:             agent,
				Rows:              termHeight,
				Cols:              termWidth,
				ScrollbackCapture: scrollback,
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
			InstanceID:  m.instanceID,
		}
		agent, err := m.agentProvider.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		// Best-effort capture of existing scrollback from the tmux pane.
		scrollback, _ := tmux.CapturePane(sessionName, opts)
		return ptyTabReattachResult{
			WorkspaceID:       string(ws.ID()),
			TabID:             tabID,
			Agent:             agent,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
		}
	}
}

// RestartActiveTab restarts a stopped or detached agent tab by creating a fresh tmux client.
func (m *Model) RestartActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return nil
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return nil
	}
	tab.mu.Lock()
	running := tab.Running
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	tab.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab is still running",
				Level:   messages.ToastInfo,
			}
		}
	}
	ws := tab.Workspace
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	m.stopPTYReader(tab)
	var existingAgent *appPty.Agent
	tab.mu.Lock()
	existingAgent = tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if existingAgent != nil {
		_ = m.agentProvider.CloseAgent(existingAgent)
	}
	tmuxOpts := m.getTmuxOptions()

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant

	return func() tea.Msg {
		// KillSession is synchronous: it calls cmd.Run() which blocks until the
		// tmux server processes the kill and returns. By the time it completes,
		// the session is fully removed from tmux's perspective.
		// The subsequent CreateAgentWithTags uses `new-session -Ads` which is
		// atomic (attach-if-exists, create-if-not), providing an additional
		// safety net in the unlikely event of cleanup lag.
		_ = tmux.KillSession(sessionName, tmuxOpts)

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
			CreatedAt:   time.Now().Unix(),
			InstanceID:  m.instanceID,
		}
		agent, err := m.agentProvider.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Best-effort capture of scrollback (empty for fresh sessions, which is fine).
		scrollback, _ := tmux.CapturePane(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID:       string(ws.ID()),
			TabID:             tabID,
			Agent:             agent,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
		}
	}
}
