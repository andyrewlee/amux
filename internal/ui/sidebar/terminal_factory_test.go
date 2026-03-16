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
