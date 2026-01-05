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
	wt := &data.Worktree{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/tmp",
		Root:   os.TempDir(),
	}

	agent, err := mgr.CreateAgent(wt, AgentCodex, data.ResumeInfo{})
	if err != nil {
		t.Fatalf("CreateAgent() error = %v", err)
	}
	if agent == nil || agent.Terminal == nil {
		t.Fatalf("CreateAgent() returned nil agent/terminal")
	}

	if len(mgr.agents[wt.ID()]) != 1 {
		t.Fatalf("expected 1 agent for worktree")
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
	wt := &data.Worktree{Root: os.TempDir()}
	agent, err := mgr.CreateAgent(wt, AgentCodex, data.ResumeInfo{})
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
