package pty

import "testing"

func TestAgentManager_NilAgentNoops(t *testing.T) {
	m := NewAgentManager(testConfig())

	if err := m.CloseAgent(nil); err != nil {
		t.Fatalf("CloseAgent(nil) error = %v", err)
	}
	if err := m.SendInterrupt(nil); err != nil {
		t.Fatalf("SendInterrupt(nil) error = %v", err)
	}
}

func TestAgentManager_CleanupSkipsNilAgentEntries(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := testWorkspace()

	m.mu.Lock()
	m.agents[ws.ID()] = []*Agent{nil}
	m.mu.Unlock()

	m.CloseWorkspaceAgents(ws)

	m.mu.Lock()
	_, stillTracked := m.agents[ws.ID()]
	m.mu.Unlock()
	if stillTracked {
		t.Fatal("expected CloseWorkspaceAgents to delete workspace entry")
	}

	m.mu.Lock()
	m.agents[ws.ID()] = []*Agent{nil}
	m.mu.Unlock()

	m.CloseAll()

	m.mu.Lock()
	count := len(m.agents)
	m.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected CloseAll to clear agents map, got %d entries", count)
	}
}
