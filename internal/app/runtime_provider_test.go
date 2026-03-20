package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

type stubSandboxRuntimeManager struct {
	createAgentWithTagsCalls []stubCreateAgentWithTagsCall
	syncToLocalCalls         []*data.Workspace
	syncToLocalErr           error
	tmuxOptions              tmux.Options
}

type stubCreateAgentWithTagsCall struct {
	workspace   *data.Workspace
	agentType   pty.AgentType
	sessionName string
	rows        uint16
	cols        uint16
	tags        tmux.SessionTags
}

func (s *stubSandboxRuntimeManager) CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error) {
	return &pty.Agent{Workspace: wt, Type: agentType}, nil
}

func (s *stubSandboxRuntimeManager) CreateAgentWithTags(wt *data.Workspace, agentType pty.AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	s.createAgentWithTagsCalls = append(s.createAgentWithTagsCalls, stubCreateAgentWithTagsCall{
		workspace:   wt,
		agentType:   agentType,
		sessionName: sessionName,
		rows:        rows,
		cols:        cols,
		tags:        tags,
	})
	return &pty.Agent{Workspace: wt, Type: agentType, Session: sessionName}, nil
}

func (s *stubSandboxRuntimeManager) CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error) {
	return &pty.Agent{Workspace: wt, Type: pty.AgentType("viewer")}, nil
}

func (s *stubSandboxRuntimeManager) CreateViewerWithTags(wt *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	return &pty.Agent{Workspace: wt, Type: pty.AgentType("viewer"), Session: sessionName}, nil
}

func (s *stubSandboxRuntimeManager) CreateShell(wt *data.Workspace) (*pty.Terminal, error) {
	return nil, nil
}

func (s *stubSandboxRuntimeManager) SyncToLocal(wt *data.Workspace) error {
	s.syncToLocalCalls = append(s.syncToLocalCalls, wt)
	return s.syncToLocalErr
}

func (s *stubSandboxRuntimeManager) SyncAllToLocal() error { return nil }

func (s *stubSandboxRuntimeManager) SetTmuxOptions(opts tmux.Options) {
	s.tmuxOptions = opts
}

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

func TestRuntimeAgentProviderCreateAgentWithTagsUsesSandboxSessionName(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox
	sandboxMgr := &stubSandboxRuntimeManager{}
	provider := &RuntimeAgentProvider{
		local:         pty.NewAgentManager(&config.Config{}),
		sandbox:       sandboxMgr,
		sandboxAgents: make(map[*pty.Agent]struct{}),
	}
	tags := tmux.SessionTags{WorkspaceID: string(ws.ID()), TabID: "tab-1"}

	agent, err := provider.CreateAgentWithTags(ws, pty.AgentCodex, "sess-reattach", 24, 80, tags)
	if err != nil {
		t.Fatalf("CreateAgentWithTags() error = %v", err)
	}
	if agent.Session != "sess-reattach" {
		t.Fatalf("agent.Session = %q, want %q", agent.Session, "sess-reattach")
	}
	if len(sandboxMgr.createAgentWithTagsCalls) != 1 {
		t.Fatalf("CreateAgentWithTags calls = %d, want 1", len(sandboxMgr.createAgentWithTagsCalls))
	}
	call := sandboxMgr.createAgentWithTagsCalls[0]
	if call.sessionName != "sess-reattach" {
		t.Fatalf("sessionName = %q, want %q", call.sessionName, "sess-reattach")
	}
	if call.tags.TabID != "tab-1" {
		t.Fatalf("tags.TabID = %q, want %q", call.tags.TabID, "tab-1")
	}
}
