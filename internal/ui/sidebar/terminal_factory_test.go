package sidebar

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
)

func TestCreateTerminalTabUsesFactoryForCloudRuntime(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox

	called := false
	m.SetTerminalFactory(func(got *data.Workspace) (*pty.Terminal, error) {
		called = true
		if got != ws {
			t.Fatalf("factory workspace = %p, want %p", got, ws)
		}
		return nil, nil
	})

	cmd := m.createTerminalTab(ws)
	if cmd == nil {
		t.Fatal("expected createTerminalTab cmd")
	}
	msg := cmd()
	if !called {
		t.Fatal("expected terminal factory to be called")
	}
	created, ok := msg.(SidebarTerminalCreated)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreated, got %T", msg)
	}
	if created.WorkspaceID != string(ws.ID()) {
		t.Fatalf("WorkspaceID = %q, want %q", created.WorkspaceID, ws.ID())
	}
	if created.Workspace != ws {
		t.Fatalf("Workspace = %p, want %p", created.Workspace, ws)
	}
}

func TestCreateTerminalTabFactoryError(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox

	wantErr := errors.New("factory failed")
	m.SetTerminalFactory(func(*data.Workspace) (*pty.Terminal, error) {
		return nil, wantErr
	})

	cmd := m.createTerminalTab(ws)
	if cmd == nil {
		t.Fatal("expected createTerminalTab cmd")
	}
	msg := cmd()
	failed, ok := msg.(SidebarTerminalCreateFailed)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreateFailed, got %T", msg)
	}
	if !errors.Is(failed.Err, wantErr) {
		t.Fatalf("error = %v, want %v", failed.Err, wantErr)
	}
}

func TestReattachActiveTabUsesFactoryForCloudRuntime(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				Running:  false,
				Detached: true,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0

	called := false
	m.SetTerminalFactory(func(got *data.Workspace) (*pty.Terminal, error) {
		called = true
		if got != ws {
			t.Fatalf("factory workspace = %p, want %p", got, ws)
		}
		return nil, nil
	})

	cmd := m.ReattachActiveTab()
	if cmd == nil {
		t.Fatal("expected reattach command")
	}
	msg := cmd()
	if !called {
		t.Fatal("expected terminal factory to be called")
	}
	if _, ok := msg.(SidebarTerminalReattachResult); !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
}

func TestHandleTerminalCreatedPreservesOriginalWorkspaceAcrossAsyncSwitch(t *testing.T) {
	m := NewTerminalModel()
	sandboxWS := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	sandboxWS.Runtime = data.RuntimeCloudSandbox
	localWS := data.NewWorkspace("other", "main", "main", "/repo/other", "/repo/other")
	wsID := string(sandboxWS.ID())

	m.workspace = sandboxWS
	m.SetTerminalFactory(func(got *data.Workspace) (*pty.Terminal, error) {
		if got != sandboxWS {
			t.Fatalf("factory workspace = %p, want %p", got, sandboxWS)
		}
		return &pty.Terminal{}, nil
	})

	cmd := m.createTerminalTab(sandboxWS)
	if cmd == nil {
		t.Fatal("expected createTerminalTab cmd")
	}
	msg, ok := cmd().(SidebarTerminalCreated)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreated, got %T", cmd())
	}

	m.workspace = localWS
	if follow := m.handleTerminalCreated(msg); follow != nil {
		_ = follow
	}

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(tabs))
	}
	if tabs[0].Workspace != sandboxWS {
		t.Fatalf("tab workspace = %p, want original sandbox workspace %p", tabs[0].Workspace, sandboxWS)
	}
}

func TestRestartActiveTabUsesFactoryForCloudRuntime(t *testing.T) {
	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	ws.Runtime = data.RuntimeCloudSandbox
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	m.workspace = ws
	m.tabsByWorkspace[wsID] = []*TerminalTab{
		{
			ID: tabID,
			State: &TerminalState{
				Running:  false,
				Detached: true,
			},
		},
	}
	m.activeTabByWorkspace[wsID] = 0

	called := false
	m.SetTerminalFactory(func(got *data.Workspace) (*pty.Terminal, error) {
		called = true
		if got != ws {
			t.Fatalf("factory workspace = %p, want %p", got, ws)
		}
		return nil, nil
	})

	cmd := m.RestartActiveTab()
	if cmd == nil {
		t.Fatal("expected restart command")
	}
	msg := cmd()
	if !called {
		t.Fatal("expected terminal factory to be called")
	}
	if _, ok := msg.(SidebarTerminalReattachResult); !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
}
