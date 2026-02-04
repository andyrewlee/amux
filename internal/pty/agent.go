package pty

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// AgentType represents the type of AI agent
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentGemini   AgentType = "gemini"
	AgentAmp      AgentType = "amp"
	AgentOpencode AgentType = "opencode"
	AgentDroid    AgentType = "droid"
	AgentCline    AgentType = "cline"
	AgentCursor   AgentType = "cursor"
	AgentPi       AgentType = "pi"
)

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
	config *config.Config
	mu     sync.Mutex
	agents map[data.WorkspaceID][]*Agent
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config) *AgentManager {
	return &AgentManager{
		config: cfg,
		agents: make(map[data.WorkspaceID][]*Agent),
	}
}

// CreateAgent creates a new agent for the given workspace.
func (m *AgentManager) CreateAgent(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateAgentWithTags(ws, agentType, sessionName, rows, cols, tmux.SessionTags{})
}

// CreateAgentWithTags creates a new agent for the given workspace with tmux tags.
func (m *AgentManager) CreateAgentWithTags(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
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
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}

	// Create terminal with agent command, falling back to shell on exit
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Execute agent, then reset terminal state and drop to shell
	// Reset sequence: stty sane (terminal modes), exit alt screen, show cursor, reset attrs, RIS
	// Use -l flag to start login shell so .zshrc/.bashrc are loaded
	fullCommand := fmt.Sprintf("%s; stty sane; printf '\\033[?1049l\\033[?25h\\033[0m\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; exec %s -l", assistantCfg.Command, shell)

	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root, fullCommand, tmux.DefaultOptions(), tags)
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
func (m *AgentManager) CreateViewer(ws *data.Workspace, command string, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateViewerWithTags(ws, command, sessionName, rows, cols, tmux.SessionTags{})
}

// CreateViewerWithTags creates a new viewer for the given workspace with tmux tags.
func (m *AgentManager) CreateViewerWithTags(ws *data.Workspace, command string, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), "viewer")
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}
	// Build environment
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}

	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root, command, tmux.DefaultOptions(), tags)
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
	if agent.Terminal != nil {
		agent.Terminal.Close()
	}

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

	for _, agents := range agentsByWorkspace {
		for _, agent := range agents {
			if agent.Terminal != nil {
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
	for _, agent := range agents {
		if agent.Terminal != nil {
			agent.Terminal.Close()
		}
	}
}

// SendInterrupt sends an interrupt to an agent
func (m *AgentManager) SendInterrupt(agent *Agent) error {
	if agent.Terminal == nil {
		return nil
	}

	// Send multiple interrupts if configured (e.g., for Claude)
	for i := 0; i < agent.Config.InterruptCount; i++ {
		if err := agent.Terminal.SendInterrupt(); err != nil {
			return err
		}
		// Add delay between interrupts if configured
		if i < agent.Config.InterruptCount-1 && agent.Config.InterruptDelayMs > 0 {
			time.Sleep(time.Duration(agent.Config.InterruptDelayMs) * time.Millisecond)
		}
	}

	return nil
}
