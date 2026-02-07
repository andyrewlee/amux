package sidebar

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// createTerminalTab creates a new terminal tab for the workspace
func (m *TerminalModel) createTerminalTab(ws *data.Workspace) tea.Cmd {
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	termWidth, termHeight := m.terminalContentSize()
	opts := m.getTmuxOptions()
	instanceID := m.instanceID
	root := ws.Root

	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		if err := tmux.EnsureAvailable(); err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		var scrollback []byte
		env := []string{"COLORTERM=truecolor"}
		sessionName := tmux.SessionName("amux", wsID, string(tabID))
		// Reuse scrollback if a prior tmux session with the same name exists
		// (e.g., app restart with persisted tmux session).
		if state, err := tmux.SessionStateFor(sessionName, opts); err == nil && state.Exists && state.HasLivePane {
			scrollback, _ = tmux.CapturePane(sessionName, opts)
		}
		tags := tmux.SessionTags{
			WorkspaceID: wsID,
			TabID:       string(tabID),
			Type:        "terminal",
			Assistant:   "terminal",
			CreatedAt:   time.Now().Unix(),
			InstanceID:  instanceID,
		}
		command := tmux.ClientCommandWithTagsAttach(sessionName, root, fmt.Sprintf("exec %s -l", shell), opts, tags, true)
		term, err := pty.NewWithSize(command, root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		return SidebarTerminalCreated{
			WorkspaceID: wsID,
			TabID:       tabID,
			Terminal:    term,
			SessionName: sessionName,
			Scrollback:  scrollback,
		}
	}
}

// DetachActiveTab closes the PTY client but keeps the tmux session alive.
func (m *TerminalModel) DetachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil {
		return nil
	}
	m.detachState(tab.State)
	return nil
}

// ReattachActiveTab reattaches to a detached tmux session for the active terminal tab.
func (m *TerminalModel) ReattachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	return m.attachToSession(ws, tab.ID, sessionName, true, "reattach")
}

// RestartActiveTab starts a fresh tmux session for the active terminal tab.
func (m *TerminalModel) RestartActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	m.detachState(ts)
	_ = tmux.KillSession(sessionName, m.getTmuxOptions())
	return m.attachToSession(ws, tab.ID, sessionName, true, "restart")
}

func (m *TerminalModel) attachToSession(ws *data.Workspace, tabID TerminalTabID, sessionName string, detachExisting bool, action string) tea.Cmd {
	if ws == nil {
		return nil
	}
	// Snapshot model-dependent values so the async cmd doesn't race on TerminalModel fields.
	opts := m.getTmuxOptions()
	termWidth, termHeight := m.terminalContentSize()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	env := []string{"COLORTERM=truecolor"}
	wsID := string(ws.ID())
	root := ws.Root
	instanceID := m.instanceID
	return func() tea.Msg {
		if err := tmux.EnsureAvailable(); err != nil {
			return SidebarTerminalReattachFailed{
				WorkspaceID: wsID,
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		if action == "reattach" {
			state, err := tmux.SessionStateFor(sessionName, opts)
			if err != nil {
				return SidebarTerminalReattachFailed{
					WorkspaceID: wsID,
					TabID:       tabID,
					Err:         err,
					Action:      action,
				}
			}
			if !state.Exists || !state.HasLivePane {
				return SidebarTerminalReattachFailed{
					WorkspaceID: wsID,
					TabID:       tabID,
					Err:         errors.New("tmux session ended"),
					Stopped:     true,
					Action:      action,
				}
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID: wsID,
			TabID:       string(tabID),
			Type:        "terminal",
			Assistant:   "terminal",
			InstanceID:  instanceID,
		}
		if action == "restart" {
			tags.CreatedAt = time.Now().Unix()
		}
		command := tmux.ClientCommandWithTagsAttach(sessionName, root, fmt.Sprintf("exec %s -l", shell), opts, tags, detachExisting)
		term, err := pty.NewWithSize(command, root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return SidebarTerminalReattachFailed{
				WorkspaceID: wsID,
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		scrollback, _ := tmux.CapturePane(sessionName, opts)
		return SidebarTerminalReattachResult{
			WorkspaceID: wsID,
			TabID:       tabID,
			Terminal:    term,
			SessionName: sessionName,
			Scrollback:  scrollback,
		}
	}
}
