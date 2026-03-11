package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
)

func TestRuntimeAgentProviderCloseAllClosesSandboxAgents(t *testing.T) {
	term, err := pty.New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("pty.New() error = %v", err)
	}
	agent := &pty.Agent{Terminal: term}

	provider := &RuntimeAgentProvider{
		local:         pty.NewAgentManager(&config.Config{}),
		sandboxAgents: map[*pty.Agent]struct{}{agent: {}},
	}

	provider.CloseAll()

	if !term.IsClosed() {
		t.Fatal("expected sandbox terminal to be closed")
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if got := len(provider.sandboxAgents); got != 0 {
		t.Fatalf("sandboxAgents size = %d, want 0", got)
	}
}

func TestRuntimeAgentProviderCloseAgentClosesTrackedSandboxAgent(t *testing.T) {
	term, err := pty.New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("pty.New() error = %v", err)
	}
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox
	agent := &pty.Agent{
		Terminal:  term,
		Workspace: ws,
	}

	provider := &RuntimeAgentProvider{
		local:         pty.NewAgentManager(&config.Config{}),
		sandboxAgents: map[*pty.Agent]struct{}{agent: {}},
	}

	if err := provider.CloseAgent(agent); err != nil {
		t.Fatalf("CloseAgent() error = %v", err)
	}
	if !term.IsClosed() {
		t.Fatal("expected sandbox terminal to be closed")
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if got := len(provider.sandboxAgents); got != 0 {
		t.Fatalf("sandboxAgents size = %d, want 0", got)
	}
}
