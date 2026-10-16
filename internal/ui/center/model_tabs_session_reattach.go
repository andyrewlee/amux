package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// These package-level indirections are test seams for reattach/bootstrap
// paths. The bootstrap seams themselves live in ptyio (ptyio.ProbeSessionFn
// et al.); tests that override any of them must not use t.Parallel within
// this package.
var (
	sessionStateForFn       = tmux.SessionStateFor
	sessionOwnedFn          = ptyio.SessionOwned
	capturePaneFn           = tmux.CapturePane
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		return manager.CreateAgentWithConfig(ws, agentType, sessionName, rows, cols, tags, cfg)
	}
)

func (m *Model) sessionBootstrapViewportSize() (int, int) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0
	}
	tm := m.terminalMetrics()
	return tm.Width, tm.Height
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
	var running, detached, reattachInFlight bool
	var sessionName string
	var epoch uint64
	canReattach := false
	withMu(&tab.mu, func() {
		running = tab.Running
		detached = tab.Detached
		reattachInFlight = tab.Reattach.InFlight
		sessionName = tab.SessionName
		canReattach = detached || !running
		if canReattach && !reattachInFlight {
			_ = tab.beginReattachLocked()
		}
		epoch = tab.reattachEpochLocked()
	})
	if !canReattach {
		return nil
	}
	if reattachInFlight {
		// Say so rather than no-op. A reattach that is genuinely in flight
		// resolves on its own or is released by SweepStalledReattaches, but a
		// silent keybinding makes a stuck tab look like a broken one.
		return func() tea.Msg {
			return messages.Toast{
				Message: "Reattach already in progress",
				Level:   messages.ToastInfo,
			}
		}
	}
	if m.config == nil || m.config.Assistants == nil {
		withMu(&tab.mu, func() {
			tab.endReattachLocked()
		})
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	assistantCfg, ok := m.config.Assistants[tab.Assistant]
	if !ok {
		withMu(&tab.mu, func() {
			tab.endReattachLocked()
		})
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	tm := m.terminalMetrics()
	attachWidth := tm.Width
	attachHeight := tm.Height
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(tab.Workspace.ID()), string(tab.ID))
	}
	assistant := tab.Assistant
	ws := tab.Workspace
	tabID := tab.ID
	return func() tea.Msg {
		return m.attachSessionPipeline(attachPipelineParams{
			ws:           ws,
			tabID:        tabID,
			assistant:    assistant,
			assistantCfg: assistantCfg,
			sessionName:  sessionName,
			epoch:        epoch,
			termWidth:    termWidth,
			termHeight:   termHeight,
			attachWidth:  attachWidth,
			attachHeight: attachHeight,
		})
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
	assistantCfg, ok := m.config.Assistants[tab.Assistant]
	if !ok {
		return nil
	}
	var running, reattachInFlight bool
	var sessionName string
	var epoch uint64
	withMu(&tab.mu, func() {
		running = tab.Running
		reattachInFlight = tab.Reattach.InFlight
		sessionName = tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
		if !running && !reattachInFlight {
			_ = tab.beginReattachLocked()
		}
		epoch = tab.reattachEpochLocked()
	})
	if running {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab is still running",
				Level:   messages.ToastInfo,
			}
		}
	}
	if reattachInFlight {
		return nil
	}
	ws := tab.Workspace
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	m.stopPTYReader(tab)
	var existingAgent *appPty.Agent
	withMu(&tab.mu, func() {
		existingAgent = tab.Agent
		tab.Agent = nil
		tab.retireTabInputWriterLocked()
	})
	if existingAgent != nil {
		// Closing the terminal unblocks the retired writer's in-flight send.
		_ = m.agentManager.CloseAgent(existingAgent)
	}
	tab.joinRetiredInputWriters()
	tmuxOpts := m.tmuxOpts

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant

	return func() tea.Msg {
		// KillSession is synchronous: it calls cmd.Run() which blocks until the
		// tmux server processes the kill and returns. By the time it completes,
		// the session is fully removed from tmux's perspective.
		// The subsequent CreateAgentWithConfig uses `new-session -Ads` which is
		// atomic (attach-if-exists, create-if-not), providing an additional
		// safety net in the unlikely event of cleanup lag.
		if err := ptyio.KillSessionFn(sessionName, tmuxOpts); err != nil {
			// new-session -Ads below reattaches the survivor when the kill
			// failed — a restart that silently keeps the old session must be
			// diagnosable.
			logging.Error("Failed to kill tmux session %s during agent restart: %v", sessionName, err)
		}

		tags := ptyio.AttachSessionTags(ws, string(tabID), "agent", assistant, m.instanceID, true)
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(termHeight, termWidth)
		agent, err := createAgentWithConfigFn(
			m.agentManager,
			ws,
			appPty.AgentType(assistant),
			sessionName,
			ptyRows,
			ptyCols,
			tags,
			assistantCfg,
		)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Fresh restarts must avoid seeding the visible screen before the PTY
		// reader drains unread startup bytes from the newly attached client.
		captureCols, captureRows := ptyio.DefaultBootstrap().HistoryCaptureSize(sessionName, termWidth, termHeight, tmuxOpts)
		scrollback, _ := capturePaneFn(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Epoch:       epoch,
			Agent:       agent,
			Rows:        captureRows,
			Cols:        captureCols,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture: scrollback,
				CaptureFullPane:   false,
				SnapshotCols:      termWidth,
				SnapshotRows:      termHeight,
			},
		}
	}
}
