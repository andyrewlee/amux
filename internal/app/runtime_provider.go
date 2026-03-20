package app

import (
	"errors"
	"os"
	"sync"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

type sandboxRuntimeManager interface {
	CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error)
	CreateAgentWithTags(wt *data.Workspace, agentType pty.AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error)
	CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error)
	CreateViewerWithTags(wt *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error)
	CreateShell(wt *data.Workspace) (*pty.Terminal, error)
	SyncToLocal(wt *data.Workspace) error
	SyncAllToLocal() error
	SetTmuxOptions(opts tmux.Options)
}

// RuntimeAgentProvider routes agent creation based on workspace runtime.
type RuntimeAgentProvider struct {
	local   *pty.AgentManager
	sandbox sandboxRuntimeManager

	mu            sync.Mutex
	sandboxAgents map[*pty.Agent]struct{}
}

func NewRuntimeAgentProvider(cfg *config.Config, sandboxManager *SandboxManager) *RuntimeAgentProvider {
	return &RuntimeAgentProvider{
		local:         pty.NewAgentManager(cfg),
		sandbox:       sandboxManager,
		sandboxAgents: make(map[*pty.Agent]struct{}),
	}
}

func (p *RuntimeAgentProvider) CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		agent, err := p.sandbox.CreateAgent(wt, agentType, rows, cols)
		if err == nil {
			p.trackSandboxAgent(agent)
		}
		return agent, err
	}
	return p.local.CreateAgent(wt, agentType, "", rows, cols)
}

func (p *RuntimeAgentProvider) CreateAgentWithTags(wt *data.Workspace, agentType pty.AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		agent, err := p.sandbox.CreateAgentWithTags(wt, agentType, sessionName, rows, cols, tags)
		if err == nil {
			p.trackSandboxAgent(agent)
		}
		return agent, err
	}
	return p.local.CreateAgentWithTags(wt, agentType, sessionName, rows, cols, tags)
}

func (p *RuntimeAgentProvider) CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		agent, err := p.sandbox.CreateViewer(wt, command, rows, cols)
		if err == nil {
			p.trackSandboxAgent(agent)
		}
		return agent, err
	}
	return p.local.CreateViewer(wt, command, "", rows, cols)
}

func (p *RuntimeAgentProvider) CreateViewerWithTags(wt *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	if wt != nil && data.NormalizeRuntime(wt.Runtime) == data.RuntimeCloudSandbox {
		agent, err := p.sandbox.CreateViewerWithTags(wt, command, sessionName, rows, cols, tags)
		if err == nil {
			p.trackSandboxAgent(agent)
		}
		return agent, err
	}
	return p.local.CreateViewerWithTags(wt, command, sessionName, rows, cols, tags)
}

func (p *RuntimeAgentProvider) CloseAgent(agent *pty.Agent) error {
	if agent == nil {
		return nil
	}
	// Local manager removes local agents from its list; sandbox agents are tracked here.
	if agent.Workspace != nil && data.NormalizeRuntime(agent.Workspace.Runtime) == data.RuntimeCloudSandbox {
		p.untrackSandboxAgent(agent)
		if agent.Terminal != nil {
			return agent.Terminal.Close()
		}
		return nil
	}
	return p.local.CloseAgent(agent)
}

func (p *RuntimeAgentProvider) CloseAll() {
	p.local.CloseAll()
	for _, agent := range p.drainSandboxAgents() {
		if agent != nil && agent.Terminal != nil {
			_ = agent.Terminal.Close()
		}
	}
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

func (p *RuntimeAgentProvider) SetTmuxOptions(opts tmux.Options) {
	if p.local != nil {
		p.local.SetTmuxOptions(opts)
	}
	if p.sandbox != nil {
		p.sandbox.SetTmuxOptions(opts)
	}
}

func (p *RuntimeAgentProvider) trackSandboxAgent(agent *pty.Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	if p.sandboxAgents == nil {
		p.sandboxAgents = make(map[*pty.Agent]struct{})
	}
	p.sandboxAgents[agent] = struct{}{}
	p.mu.Unlock()
}

func (p *RuntimeAgentProvider) untrackSandboxAgent(agent *pty.Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	delete(p.sandboxAgents, agent)
	p.mu.Unlock()
}

func (p *RuntimeAgentProvider) drainSandboxAgents() []*pty.Agent {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sandboxAgents) == 0 {
		return nil
	}
	agents := make([]*pty.Agent, 0, len(p.sandboxAgents))
	for agent := range p.sandboxAgents {
		agents = append(agents, agent)
	}
	p.sandboxAgents = make(map[*pty.Agent]struct{})
	return agents
}
