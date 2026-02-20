package app

import (
	"errors"
	"os"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// RuntimeAgentProvider routes agent creation based on workspace runtime.
type RuntimeAgentProvider struct {
	local   *pty.AgentManager
	sandbox *SandboxManager
}

func NewRuntimeAgentProvider(cfg *config.Config, sandboxManager *SandboxManager) *RuntimeAgentProvider {
	return &RuntimeAgentProvider{
		local:   pty.NewAgentManager(cfg),
		sandbox: sandboxManager,
	}
}

func (p *RuntimeAgentProvider) CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		return p.sandbox.CreateAgent(wt, agentType, rows, cols)
	}
	return p.local.CreateAgent(wt, agentType, "", rows, cols)
}

func (p *RuntimeAgentProvider) CreateAgentWithTags(wt *data.Workspace, agentType pty.AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		return p.sandbox.CreateAgent(wt, agentType, rows, cols)
	}
	return p.local.CreateAgentWithTags(wt, agentType, sessionName, rows, cols, tags)
}

func (p *RuntimeAgentProvider) CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		return p.sandbox.CreateViewer(wt, command, rows, cols)
	}
	return p.local.CreateViewer(wt, command, "", rows, cols)
}

func (p *RuntimeAgentProvider) CreateViewerWithTags(wt *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		return p.sandbox.CreateViewer(wt, command, rows, cols)
	}
	return p.local.CreateViewerWithTags(wt, command, sessionName, rows, cols, tags)
}

func (p *RuntimeAgentProvider) CloseAgent(agent *pty.Agent) error {
	if agent == nil {
		return nil
	}
	// Local manager will remove it from its list; sandbox manager doesn't track agents.
	if agent.Workspace != nil && data.NormalizeRuntime(agent.Workspace.Runtime) == data.RuntimeCloudSandbox {
		if agent.Terminal != nil {
			return agent.Terminal.Close()
		}
		return nil
	}
	return p.local.CloseAgent(agent)
}

func (p *RuntimeAgentProvider) CloseAll() {
	p.local.CloseAll()
}

// CreateTerminalForWorkspace returns a shell terminal based on runtime.
func (p *RuntimeAgentProvider) CreateTerminalForWorkspace(wt *data.Workspace) (*pty.Terminal, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	if data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		return p.sandbox.CreateShell(wt)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	return pty.New(shell, wt.Root, nil)
}
