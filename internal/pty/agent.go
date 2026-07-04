// Package pty manages the pseudo-terminals that back hosted agents: it spawns
// agent processes (Agent) attached to a PTY (Terminal), sizes them, and
// streams their byte output to the UI for parsing by internal/vterm.
package pty

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

// AgentType represents the type of AI agent. Concrete agent identifiers are not
// enumerated here: the canonical roster lives in the config.AgentRegistry, and
// the runtime resolves agents by their assistant-name string (see
// CreateAgentWithTags). Keeping the names in one place avoids hand-synced lists.
type AgentType string

// Agent represents a running AI agent instance
type Agent struct {
	Type      AgentType
	Terminal  *Terminal
	Workspace *data.Workspace
	Config    config.AssistantConfig
	Session   string
}

// AgentManager manages agent instances
type AgentManager struct {
	config      *config.Config
	mu          sync.Mutex
	agents      map[data.WorkspaceID][]*Agent
	tmuxOptions tmux.Options
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config) *AgentManager {
	return &AgentManager{
		config:      cfg,
		agents:      make(map[data.WorkspaceID][]*Agent),
		tmuxOptions: tmux.DefaultOptions(),
	}
}

// SetTmuxOptions updates tmux options for future agent/viewer command construction.
func (m *AgentManager) SetTmuxOptions(opts tmux.Options) {
	m.mu.Lock()
	m.tmuxOptions = opts
	m.mu.Unlock()
}

func (m *AgentManager) getTmuxOptions() tmux.Options {
	m.mu.Lock()
	opts := m.tmuxOptions
	m.mu.Unlock()
	return opts
}

// CreateAgent creates a new agent for the given workspace.
func (m *AgentManager) CreateAgent(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateAgentWithTags(ws, agentType, sessionName, rows, cols, tmux.SessionTags{})
}

// CreateAgentWithTags creates a new agent for the given workspace with tmux tags.
func (m *AgentManager) CreateAgentWithTags(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
	if ws == nil {
		return nil, errors.New("workspace is required")
	}
	assistantCfg, ok := m.config.Assistants[string(agentType)]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(agentType))
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}

	// Build environment
	env := []string{
		"WORKSPACE_ROOT=" + ws.Root,
		"WORKSPACE_NAME=" + ws.Name,
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}

	// Create terminal with agent command, falling back to shell on exit
	loginShellCommand, err := LoginShellCommandFromEnv()
	if err != nil {
		return nil, err
	}

	// Execute agent, then reset terminal state and drop to shell
	// Reset sequence: stty sane (terminal modes), exit alt screen, show cursor, reset attrs, RIS
	// Use -l flag to start login shell so .zshrc/.bashrc are loaded
	fullCommand := fmt.Sprintf("%s; stty sane; printf '\\033[?1049l\\033[?25h\\033[0m\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; %s", assistantCfg.Command, loginShellCommand)

	termCommand := tmux.NewClientCommand(sessionName, tmux.ClientCommandParams{
		WorkDir:        ws.Root,
		Command:        fullCommand,
		Options:        m.getTmuxOptions(),
		Tags:           tags,
		DetachExisting: true,
	})
	term, err := NewWithSize(termCommand, ws.Root, env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: ws,
		Config:    assistantCfg,
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CreateViewer creates a new agent (viewer) for the given workspace and command.
func (m *AgentManager) CreateViewer(ws *data.Workspace, command, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateViewerWithTags(ws, command, sessionName, rows, cols, tmux.SessionTags{})
}

// CreateViewerWithTags creates a new viewer for the given workspace with tmux tags.
func (m *AgentManager) CreateViewerWithTags(ws *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
	if ws == nil {
		return nil, errors.New("workspace is required")
	}
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), "viewer")
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}
	// Build environment
	env := []string{
		"WORKSPACE_ROOT=" + ws.Root,
		"WORKSPACE_NAME=" + ws.Name,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}

	termCommand := tmux.NewClientCommand(sessionName, tmux.ClientCommandParams{
		WorkDir:        ws.Root,
		Command:        command,
		Options:        m.getTmuxOptions(),
		Tags:           tags,
		DetachExisting: true,
	})
	term, err := NewWithSize(termCommand, ws.Root, env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      AgentType("viewer"),
		Terminal:  term,
		Workspace: ws,
		Config:    config.AssistantConfig{}, // No specific config
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CloseAgent closes an agent
func (m *AgentManager) CloseAgent(agent *Agent) error {
	if agent == nil {
		return nil
	}
	if agent.Terminal != nil {
		agent.Terminal.Close()
	}

	wsID := ""
	if agent.Workspace != nil {
		wsID = string(agent.Workspace.ID())
	}
	logging.Info("agent closed: type=%s session=%s workspace=%s", agent.Type, agent.Session, wsID)

	// Remove from list
	if agent.Workspace != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		agents := m.agents[agent.Workspace.ID()]
		for i, a := range agents {
			if a == agent {
				m.agents[agent.Workspace.ID()] = append(agents[:i], agents[i+1:]...)
				break
			}
		}
	}

	return nil
}

// CloseAll closes all agents
func (m *AgentManager) CloseAll() {
	m.mu.Lock()
	agentsByWorkspace := m.agents
	m.agents = make(map[data.WorkspaceID][]*Agent)
	m.mu.Unlock()

	total := 0
	for _, agents := range agentsByWorkspace {
		total += len(agents)
	}
	if total > 0 {
		logging.Info("closing all %d agents across %d workspaces", total, len(agentsByWorkspace))
	}

	for _, agents := range agentsByWorkspace {
		for _, agent := range agents {
			if agent != nil && agent.Terminal != nil {
				agent.Terminal.Close()
			}
		}
	}
}

// CloseWorkspaceAgents closes and removes all agents for a specific workspace
func (m *AgentManager) CloseWorkspaceAgents(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := ws.ID()
	m.mu.Lock()
	agents := m.agents[wsID]
	delete(m.agents, wsID)
	m.mu.Unlock()
	if len(agents) > 0 {
		logging.Info("closing %d agents for workspace %s", len(agents), wsID)
	}
	for _, agent := range agents {
		if agent != nil && agent.Terminal != nil {
			agent.Terminal.Close()
		}
	}
}

// SendInterrupt sends an interrupt to an agent
func (m *AgentManager) SendInterrupt(agent *Agent) error {
	if agent == nil || agent.Terminal == nil {
		return nil
	}

	// A single Ctrl-C is the floor: an unconfigured agent (e.g. a viewer with a
	// zero-value AssistantConfig) must still receive one interrupt, otherwise the
	// user's Ctrl-C would be silently swallowed.
	count := agent.Config.InterruptCount
	if count < 1 {
		count = 1
	}

	// Send multiple interrupts if configured (e.g., for Claude, which needs two
	// Ctrl-C presses within a short window to actually interrupt).
	for i := 0; i < count; i++ {
		if err := agent.Terminal.SendInterrupt(); err != nil {
			return err
		}
		// Add delay between interrupts if configured
		if i < count-1 && agent.Config.InterruptDelayMs > 0 {
			time.Sleep(time.Duration(agent.Config.InterruptDelayMs) * time.Millisecond)
		}
	}

	return nil
}
