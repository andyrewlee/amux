package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestDetachTab_EmitsWorkspaceAwareMessage(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	}

	cmd := m.detachTab(tab, 2)
	if cmd == nil {
		t.Fatal("expected non-nil detach cmd")
	}
	msg, ok := cmd().(messages.TabDetached)
	if !ok {
		t.Fatalf("expected messages.TabDetached, got %T", cmd())
	}
	if msg.Index != 2 {
		t.Fatalf("index = %d, want 2", msg.Index)
	}
	if msg.WorkspaceID != wsID {
		t.Fatalf("workspaceID = %q, want %q", msg.WorkspaceID, wsID)
	}

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected tab to be marked detached")
	}
	if running {
		t.Fatal("expected tab to be marked not running")
	}
}

func TestDetachTab_ClearsReattachInFlight(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		ID:               TabID("tab-2"),
		Assistant:        "claude",
		Workspace:        ws,
		Running:          true,
		reattachInFlight: true,
		Agent:            &appPty.Agent{Workspace: ws},
	}

	cmd := m.detachTab(tab, 0)
	if cmd == nil {
		t.Fatal("expected non-nil detach cmd")
	}
	_ = cmd()

	tab.mu.Lock()
	inFlight := tab.reattachInFlight
	tab.mu.Unlock()
	if inFlight {
		t.Fatal("expected reattachInFlight=false after detach")
	}
}
