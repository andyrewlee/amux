package center

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

func (m *Model) addDetachedTab(ws *data.Workspace, info data.TabInfo) {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	term.AllowAltScreenScrollback = true
	ca := info.CreatedAt
	if ca == 0 {
		ca = time.Now().Unix()
	}
	tab := &Tab{
		ID:            generateTabID(),
		Name:          displayName,
		Assistant:     info.Assistant,
		Workspace:     ws,
		SessionName:   info.SessionName,
		Detached:      true,
		Running:       false,
		Terminal:      term,
		createdAt:     ca,
		lastFocusedAt: time.Unix(ca, 0),
	}
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
}

// addPlaceholderTab synchronously creates a placeholder tab in the correct slice
// position. The tab starts detached and non-running; an async reattach upgrades
// it in-place (by TabID) without changing slice order.
func (m *Model) addPlaceholderTab(ws *data.Workspace, info data.TabInfo) (TabID, string) {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	term.AllowAltScreenScrollback = true
	tabID := generateTabID()
	sessionName := strings.TrimSpace(info.SessionName)
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	ca := info.CreatedAt
	if ca == 0 {
		ca = time.Now().Unix()
	}
	tab := &Tab{
		ID:          tabID,
		Name:        displayName,
		Assistant:   info.Assistant,
		Workspace:   ws,
		SessionName: sessionName,
		Detached:    true,
		Running:     false,
		// Placeholder tabs are immediately queued for async reattach.
		reattachInFlight: true,
		Terminal:         term,
		createdAt:        ca,
		lastFocusedAt:    time.Unix(ca, 0),
	}
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
	return tabID, sessionName
}

// reattachToSession returns a tea.Cmd that asynchronously connects a placeholder
// tab to its tmux session. On success it produces ptyTabReattachResult which
// updates the tab in-place (by TabID). On failure it produces ptyTabReattachFailed.
func (m *Model) reattachToSession(ws *data.Workspace, tabID TabID, assistant, sessionName string) tea.Cmd {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
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
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         errors.New("tmux session ended"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID:  string(ws.ID()),
			TabID:        string(tabID),
			Type:         "agent",
			Assistant:    assistant,
			InstanceID:   m.instanceID,
			SessionOwner: m.instanceID,
			LeaseAtMS:    time.Now().UnixMilli(),
		}
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
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
}
