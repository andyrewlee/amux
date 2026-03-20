package center

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// newReattachTags builds the common tmux session tags for reattach/restart operations.
func (m *Model) newReattachTags(ws *data.Workspace, tabID TabID, assistant string, includeCreatedAt bool) tmux.SessionTags {
	tags := tmux.SessionTags{
		WorkspaceID:  string(ws.ID()),
		TabID:        string(tabID),
		Type:         "agent",
		Assistant:    assistant,
		InstanceID:   m.instanceID,
		SessionOwner: m.instanceID,
		LeaseAtMS:    time.Now().UnixMilli(),
	}
	if includeCreatedAt {
		tags.CreatedAt = time.Now().Unix()
	}
	return tags
}

// createAgentAndCapture creates an agent with tags, captures scrollback, and returns
// the appropriate result or failure message.
func (m *Model) createAgentAndCapture(
	ws *data.Workspace, tabID TabID, assistant, sessionName string,
	h, w int, tags tmux.SessionTags, opts tmux.Options,
	action string, stopped bool,
) tea.Msg {
	agent, err := m.agentProvider.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(h), uint16(w), tags)
	if err != nil {
		return ptyTabReattachFailed{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Err:         err,
			Stopped:     stopped,
			Action:      action,
		}
	}
	captureSessionName := sessionName
	if strings.TrimSpace(agent.Session) != "" {
		captureSessionName = agent.Session
	}
	scrollback, _ := tmux.CapturePane(captureSessionName, opts)
	return ptyTabReattachResult{
		WorkspaceID:       string(ws.ID()),
		TabID:             tabID,
		Agent:             agent,
		Rows:              h,
		Cols:              w,
		ScrollbackCapture: scrollback,
	}
}

// ReattachActiveTab reattaches to a detached/stopped tmux session.
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
	running := tab.Running
	detached := tab.Detached
	reattachInFlight := tab.reattachInFlight
	sessionName := tab.SessionName
	canReattach := detached || !running
	if canReattach && !reattachInFlight {
		tab.reattachInFlight = true
	}
	tab.mu.Unlock()
	if !canReattach {
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
		sessionName = defaultSessionName(tab.Workspace, string(tab.ID))
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
			tags := m.newReattachTags(ws, tabID, assistant, true)
			return m.createAgentAndCapture(ws, tabID, assistant, sessionName, termHeight, termWidth, tags, opts, "reattach", true)
		}
		tags := m.newReattachTags(ws, tabID, assistant, false)
		return m.createAgentAndCapture(ws, tabID, assistant, sessionName, termHeight, termWidth, tags, opts, "reattach", false)
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
		sessionName = defaultSessionName(ws, string(tabID))
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
		_ = tmux.KillSession(sessionName, tmuxOpts)
		tags := m.newReattachTags(ws, tabID, assistant, true)
		return m.createAgentAndCapture(ws, tabID, assistant, sessionName, termHeight, termWidth, tags, tmuxOpts, "restart", true)
	}
}
