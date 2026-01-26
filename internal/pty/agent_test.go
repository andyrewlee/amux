package pty

import (
	"os"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
)

func TestAgentManagerCreateAndClose(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	// Override assistant commands to use a safe shell command.
	for k, v := range cfg.Assistants {
		v.Command = "sh -c 'echo ok'"
		cfg.Assistants[k] = v
	}

	mgr := NewAgentManager(cfg)
	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/tmp",
		Root:   os.TempDir(),
	}

	agent, err := mgr.CreateAgent(wt, AgentCodex, 24, 80)
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if agent == nil || agent.Terminal == nil {
		t.Fatalf("CreateAgent() returned nil agent/terminal")
	}

	if len(mgr.agents[wt.ID()]) != 1 {
		t.Fatalf("expected 1 agent for workspace")
	}

	if err := mgr.CloseAgent(agent); err != nil {
		t.Fatalf("CloseAgent() error = %v", err)
	}
	if len(mgr.agents[wt.ID()]) != 0 {
		t.Fatalf("expected 0 agents after close")
	}
}

func TestAgentManagerSendInterrupt(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	cfg.Assistants["codex"] = config.AssistantConfig{Command: "sh -c 'sleep 1'", InterruptCount: 1}

	mgr := NewAgentManager(cfg)
	wt := &data.Workspace{Root: os.TempDir()}
	agent, err := mgr.CreateAgent(wt, AgentCodex, 24, 80)
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	defer func() {
		_ = mgr.CloseAgent(agent)
	}()

	if err := mgr.SendInterrupt(agent); err != nil {
		t.Fatalf("SendInterrupt() error = %v", err)
	}
}

func TestAgentManagerCloseWorkspaceAgents(t *testing.T) {
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}

	mgr := NewAgentManager(cfg)
	wt1 := &data.Workspace{Name: "wt1", Repo: "/tmp", Root: "/tmp/wt1"}
	wt2 := &data.Workspace{Name: "wt2", Repo: "/tmp", Root: "/tmp/wt2"}

	// Directly populate agents map to test cleanup without creating real terminals
	mgr.agents[wt1.ID()] = []*Agent{
		{Type: AgentCodex, Workspace: wt1},
		{Type: AgentClaude, Workspace: wt1},
	}
	mgr.agents[wt2.ID()] = []*Agent{
		{Type: AgentCodex, Workspace: wt2},
	}

	if len(mgr.agents[wt1.ID()]) != 2 {
		t.Fatalf("expected 2 agents for wt1, got %d", len(mgr.agents[wt1.ID()]))
	}
	if len(mgr.agents[wt2.ID()]) != 1 {
		t.Fatalf("expected 1 agent for wt2, got %d", len(mgr.agents[wt2.ID()]))
	}

	mgr.CloseWorkspaceAgents(wt1)

	if _, exists := mgr.agents[wt1.ID()]; exists {
		t.Fatalf("expected wt1 agents to be deleted from map")
	}
	if len(mgr.agents[wt2.ID()]) != 1 {
		t.Fatalf("expected wt2 agents unchanged, got %d", len(mgr.agents[wt2.ID()]))
	}

	// Should not panic on nil
	mgr.CloseWorkspaceAgents(nil)
}
