package pty

import (
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func testConfig() *config.Config {
	return &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"claude": {
				Command:          "echo claude",
				InterruptCount:   2,
				InterruptDelayMs: 50,
			},
			"codex": {
				Command:        "echo codex",
				InterruptCount: 1,
			},
		},
	}
}

func testWorkspace() *data.Workspace {
	return &data.Workspace{
		Name: "test-ws",
		Root: "/tmp/test-root",
		Repo: "/tmp/test-repo",
	}
}

func TestNewAgentManager(t *testing.T) {
	cfg := testConfig()
	m := NewAgentManager(cfg)

	if m == nil {
		t.Fatal("NewAgentManager returned nil")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.agents == nil {
		t.Error("agents map should be initialized")
	}
}

func TestAgentManager_SetTmuxOptions(t *testing.T) {
	m := NewAgentManager(testConfig())

	opts := tmux.Options{
		ServerName: "test-server",
		ConfigPath: "/tmp/test.conf",
		HideStatus: true,
	}
	m.SetTmuxOptions(opts)

	got := m.getTmuxOptions()
	if got.ServerName != "test-server" {
		t.Errorf("expected ServerName 'test-server', got %q", got.ServerName)
	}
	if got.ConfigPath != "/tmp/test.conf" {
		t.Errorf("expected ConfigPath '/tmp/test.conf', got %q", got.ConfigPath)
	}
	if !got.HideStatus {
		t.Error("expected HideStatus true")
	}
}

func TestAgentManager_SetTmuxOptionsConcurrent(t *testing.T) {
	m := NewAgentManager(testConfig())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			m.SetTmuxOptions(tmux.Options{ServerName: "server"})
		}(i)
		go func(n int) {
			defer wg.Done()
			_ = m.getTmuxOptions()
		}(i)
	}
	wg.Wait()
}

func TestAgentManager_CreateAgent_UnknownType(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	_, err := m.CreateAgent(ws, AgentType("nonexistent"), "", 24, 80)
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
	if got := err.Error(); got != "unknown agent type: nonexistent" {
		t.Errorf("unexpected error message: %q", got)
	}
}

func TestAgentManager_CreateViewerWithTags_NilWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())

	_, err := m.CreateViewerWithTags(nil, "echo hi", "sess", 24, 80, tmux.SessionTags{})
	if err == nil {
		t.Fatal("expected error for nil workspace")
	}
	if got := err.Error(); got != "workspace is required" {
		t.Errorf("unexpected error message: %q", got)
	}
}

func TestAgentManager_CloseAgent(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	// Manually add an agent (bypassing tmux/pty creation)
	agent := &Agent{
		Type:      AgentClaude,
		Terminal:  nil, // no real terminal
		Workspace: ws,
		Session:   "test-session",
	}

	wsID := ws.ID()
	m.mu.Lock()
	m.agents[wsID] = append(m.agents[wsID], agent)
	m.mu.Unlock()

	// Verify it was added
	m.mu.Lock()
	if len(m.agents[wsID]) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(m.agents[wsID]))
	}
	m.mu.Unlock()

	// Close it
	err := m.CloseAgent(agent)
	if err != nil {
		t.Fatalf("CloseAgent failed: %v", err)
	}

	// Verify it was removed
	m.mu.Lock()
	if len(m.agents[wsID]) != 0 {
		t.Errorf("expected 0 agents after close, got %d", len(m.agents[wsID]))
	}
	m.mu.Unlock()
}

func TestAgentManager_CloseAgent_WithTerminal(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	// Create a real terminal with a simple command
	term, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}

	agent := &Agent{
		Type:      AgentClaude,
		Terminal:  term,
		Workspace: ws,
		Session:   "test-session",
	}

	wsID := ws.ID()
	m.mu.Lock()
	m.agents[wsID] = append(m.agents[wsID], agent)
	m.mu.Unlock()

	err = m.CloseAgent(agent)
	if err != nil {
		t.Fatalf("CloseAgent failed: %v", err)
	}

	if !term.IsClosed() {
		t.Error("terminal should be closed after CloseAgent")
	}

	m.mu.Lock()
	if len(m.agents[wsID]) != 0 {
		t.Errorf("expected 0 agents after close, got %d", len(m.agents[wsID]))
	}
	m.mu.Unlock()
}

func TestAgentManager_CloseAgent_NilWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())

	agent := &Agent{
		Type:      AgentClaude,
		Terminal:  nil,
		Workspace: nil,
		Session:   "test-session",
	}

	// Should not panic with nil workspace
	err := m.CloseAgent(agent)
	if err != nil {
		t.Fatalf("CloseAgent with nil workspace failed: %v", err)
	}
}

func TestAgentManager_CloseAll(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws1 := &data.Workspace{Name: "ws1", Root: "/tmp/ws1", Repo: "/tmp/repo1"}
	ws2 := &data.Workspace{Name: "ws2", Root: "/tmp/ws2", Repo: "/tmp/repo2"}

	// Create real terminals
	term1, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create term1: %v", err)
	}
	term2, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		term1.Close()
		t.Fatalf("failed to create term2: %v", err)
	}

	agent1 := &Agent{Type: AgentClaude, Terminal: term1, Workspace: ws1, Session: "s1"}
	agent2 := &Agent{Type: AgentCodex, Terminal: term2, Workspace: ws2, Session: "s2"}

	m.mu.Lock()
	m.agents[ws1.ID()] = []*Agent{agent1}
	m.agents[ws2.ID()] = []*Agent{agent2}
	m.mu.Unlock()

	m.CloseAll()

	if !term1.IsClosed() {
		t.Error("term1 should be closed")
	}
	if !term2.IsClosed() {
		t.Error("term2 should be closed")
	}

	m.mu.Lock()
	if len(m.agents) != 0 {
		t.Errorf("expected empty agents map, got %d entries", len(m.agents))
	}
	m.mu.Unlock()
}

func TestAgentManager_CloseAll_Empty(t *testing.T) {
	m := NewAgentManager(testConfig())

	// Should not panic on empty manager
	m.CloseAll()
}

func TestAgentManager_CloseWorkspaceAgents(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws1 := &data.Workspace{Name: "ws1", Root: "/tmp/ws1", Repo: "/tmp/repo1"}
	ws2 := &data.Workspace{Name: "ws2", Root: "/tmp/ws2", Repo: "/tmp/repo2"}

	term1, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create term1: %v", err)
	}
	term2, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		term1.Close()
		t.Fatalf("failed to create term2: %v", err)
	}

	agent1 := &Agent{Type: AgentClaude, Terminal: term1, Workspace: ws1, Session: "s1"}
	agent2 := &Agent{Type: AgentCodex, Terminal: term2, Workspace: ws2, Session: "s2"}

	m.mu.Lock()
	m.agents[ws1.ID()] = []*Agent{agent1}
	m.agents[ws2.ID()] = []*Agent{agent2}
	m.mu.Unlock()

	// Close only ws1's agents
	m.CloseWorkspaceAgents(ws1)

	if !term1.IsClosed() {
		t.Error("term1 should be closed")
	}
	if term2.IsClosed() {
		t.Error("term2 should NOT be closed")
	}

	m.mu.Lock()
	if _, ok := m.agents[ws1.ID()]; ok {
		t.Error("ws1 should be removed from agents map")
	}
	if len(m.agents[ws2.ID()]) != 1 {
		t.Errorf("ws2 should still have 1 agent, got %d", len(m.agents[ws2.ID()]))
	}
	m.mu.Unlock()

	// Cleanup
	term2.Close()
}

func TestAgentManager_CloseWorkspaceAgents_NilWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())

	// Should not panic
	m.CloseWorkspaceAgents(nil)
}

func TestAgentManager_CloseWorkspaceAgents_UnknownWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	// Should not panic when workspace has no agents
	m.CloseWorkspaceAgents(ws)
}

func TestAgentManager_SendInterrupt(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentCodex,
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount: 1,
		},
	}

	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt failed: %v", err)
	}
}

func TestAgentManager_SendInterrupt_MultipleWithDelay(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentClaude,
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount:   3,
			InterruptDelayMs: 10,
		},
	}

	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with multiple interrupts failed: %v", err)
	}
}

func TestAgentManager_SendInterrupt_NilTerminal(t *testing.T) {
	m := NewAgentManager(testConfig())

	agent := &Agent{
		Type:     AgentClaude,
		Terminal: nil,
		Config: config.AssistantConfig{
			InterruptCount: 2,
		},
	}

	// Should return nil when terminal is nil
	err := m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with nil terminal should return nil, got %v", err)
	}
}

func TestAgentManager_SendInterrupt_ZeroCount(t *testing.T) {
	m := NewAgentManager(testConfig())

	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create terminal: %v", err)
	}
	defer term.Close()

	agent := &Agent{
		Type:     AgentClaude,
		Terminal: term,
		Config: config.AssistantConfig{
			InterruptCount: 0, // zero means no interrupts sent
		},
	}

	err = m.SendInterrupt(agent)
	if err != nil {
		t.Errorf("SendInterrupt with zero count should succeed, got %v", err)
	}
}

func TestAgentManager_MultipleAgentsPerWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	term1, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to create term1: %v", err)
	}
	term2, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		term1.Close()
		t.Fatalf("failed to create term2: %v", err)
	}

	agent1 := &Agent{Type: AgentClaude, Terminal: term1, Workspace: ws, Session: "s1"}
	agent2 := &Agent{Type: AgentCodex, Terminal: term2, Workspace: ws, Session: "s2"}

	wsID := ws.ID()
	m.mu.Lock()
	m.agents[wsID] = []*Agent{agent1, agent2}
	m.mu.Unlock()

	// Close only agent1
	_ = m.CloseAgent(agent1)

	m.mu.Lock()
	remaining := m.agents[wsID]
	m.mu.Unlock()

	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining agent, got %d", len(remaining))
	}
	if remaining[0] != agent2 {
		t.Error("expected agent2 to remain")
	}

	// Cleanup
	term2.Close()
}

func TestAgentType_Constants(t *testing.T) {
	// Verify agent type constants are distinct
	types := []AgentType{
		AgentClaude, AgentCodex, AgentGemini, AgentAmp,
		AgentOpencode, AgentDroid, AgentCline, AgentCursor, AgentPi,
	}
	seen := make(map[AgentType]bool)
	for _, at := range types {
		if seen[at] {
			t.Errorf("duplicate agent type: %s", at)
		}
		seen[at] = true
		if at == "" {
			t.Error("agent type should not be empty")
		}
	}
}
