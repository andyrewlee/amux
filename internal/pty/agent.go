package pty

import (
	"fmt"
	"os"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
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
)

// Agent represents a running AI agent instance
type Agent struct {
	Type     AgentType
	Terminal *Terminal
	Worktree *data.Worktree
	Config   config.AssistantConfig
}

// AgentManager manages agent instances
type AgentManager struct {
	config *config.Config
	agents map[data.WorktreeID][]*Agent
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config) *AgentManager {
	return &AgentManager{
		config: cfg,
		agents: make(map[data.WorktreeID][]*Agent),
	}
}

// CreateAgent creates a new agent for the given worktree
func (m *AgentManager) CreateAgent(wt *data.Worktree, agentType AgentType) (*Agent, error) {
	assistantCfg, ok := m.config.Assistants[string(agentType)]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}

	// Build environment
	env := []string{
		fmt.Sprintf("WORKTREE_ROOT=%s", wt.Root),
		fmt.Sprintf("WORKTREE_NAME=%s", wt.Name),
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
	}

	// Create terminal with agent command, falling back to shell on exit
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Execute agent, then reset terminal state (RIS), print message, and drop to shell
	fullCommand := fmt.Sprintf("%s; printf '\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; exec %s", assistantCfg.Command, shell)

	term, err := New(fullCommand, wt.Root, env)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:     agentType,
		Terminal: term,
		Worktree: wt,
		Config:   assistantCfg,
	}

	m.agents[wt.ID()] = append(m.agents[wt.ID()], agent)

	return agent, nil
}

// CreateViewer creates a new agent (viewer) for the given worktree and command
func (m *AgentManager) CreateViewer(wt *data.Worktree, command string) (*Agent, error) {
	if wt == nil {
		return nil, fmt.Errorf("worktree is required")
	}
	// Build environment
	env := []string{
		fmt.Sprintf("WORKTREE_ROOT=%s", wt.Root),
		fmt.Sprintf("WORKTREE_NAME=%s", wt.Name),
		"TERM=xterm-256color",
	}

	term, err := New(command, wt.Root, env)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:     AgentType("viewer"),
		Terminal: term,
		Worktree: wt,
		Config:   config.AssistantConfig{}, // No specific config
	}

	m.agents[wt.ID()] = append(m.agents[wt.ID()], agent)

	return agent, nil
}

// CloseAgent closes an agent
func (m *AgentManager) CloseAgent(agent *Agent) error {
	if agent.Terminal != nil {
		agent.Terminal.Close()
	}

	// Remove from list
	if agent.Worktree != nil {
		agents := m.agents[agent.Worktree.ID()]
		for i, a := range agents {
			if a == agent {
				m.agents[agent.Worktree.ID()] = append(agents[:i], agents[i+1:]...)
				break
			}
		}
	}

	return nil
}

// CloseAll closes all agents
func (m *AgentManager) CloseAll() {
	for _, agents := range m.agents {
		for _, agent := range agents {
			if agent.Terminal != nil {
				agent.Terminal.Close()
			}
		}
	}
	m.agents = make(map[data.WorktreeID][]*Agent)
}

// CloseWorktreeAgents closes and removes all agents for a specific worktree
func (m *AgentManager) CloseWorktreeAgents(wt *data.Worktree) {
	if wt == nil {
		return
	}
	wtID := wt.ID()
	for _, agent := range m.agents[wtID] {
		if agent.Terminal != nil {
			agent.Terminal.Close()
		}
	}
	delete(m.agents, wtID)
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
