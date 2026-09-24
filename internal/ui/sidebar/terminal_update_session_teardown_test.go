package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestHandleWorkspaceDeletedUsesStampedIDs(t *testing.T) {
	// Tabs filed under a pre-removal ID form (stamped on the message) must be
	// torn down even when ws.ID() computed now resolves differently — the
	// post-removal drift case.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	stamped := "resolved-form-id-" + string(ws.ID())

	state := &TerminalState{Terminal: &appPty.Terminal{}, Running: true}
	m := NewTerminalModel()
	m.tabs.ByWorkspace[stamped] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "drifted", State: state},
	}
	m.markPendingCreation(stamped)
	m.lastActiveAt[stamped] = time.Now()

	cmd := m.handleWorkspaceDeleted(messages.WorkspaceDeleted{
		Workspace:    ws,
		WorkspaceIDs: []string{stamped},
	})

	if cmd != nil {
		t.Fatalf("expected nil command, got %T", cmd())
	}
	if _, ok := m.tabs.ByWorkspace[stamped]; ok {
		t.Fatal("expected tabs filed under the stamped ID torn down")
	}
	if _, ok := m.pendingCreation[stamped]; ok {
		t.Fatal("expected pending-creation under the stamped ID cleared")
	}
	if _, ok := m.lastActiveAt[stamped]; ok {
		t.Fatal("expected lastActiveAt under the stamped ID cleared")
	}
}

func TestHandleWorkspaceShelvedTearsDownTabs(t *testing.T) {
	// Shelve kills the workspace's tmux sessions exactly like delete — tabs
	// keyed under the stamped IDs must come down too.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	stamped := string(ws.ID())

	state := &TerminalState{Terminal: &appPty.Terminal{}, Running: true}
	m := NewTerminalModel()
	m.tabs.ByWorkspace[stamped] = []*TerminalTab{
		{ID: generateTerminalTabID(), Name: "shelved", State: state},
	}
	m.markPendingCreation(stamped)

	cmd := m.handleWorkspaceShelved(messages.WorkspaceShelved{
		Workspace:    ws,
		WorkspaceIDs: []string{stamped},
	})

	if cmd != nil {
		t.Fatalf("expected nil command, got %T", cmd())
	}
	state.mu.Lock()
	running := state.Running
	state.mu.Unlock()
	if running {
		t.Fatal("expected Running cleared on shelve")
	}
	if _, ok := m.tabs.ByWorkspace[stamped]; ok {
		t.Fatal("expected shelved workspace dropped from the tabs map")
	}
}

func TestHandleWorkspaceDeletedRoutedThroughUpdate(t *testing.T) {
	// Update must dispatch messages.WorkspaceDeleted to handleWorkspaceDeleted.
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	wsID := string(ws.ID())
	m := NewTerminalModel()
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{
		{ID: generateTerminalTabID(), State: &TerminalState{Running: true}},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0

	_, _ = m.Update(messages.WorkspaceDeleted{Workspace: ws})

	if _, ok := m.tabs.ByWorkspace[wsID]; ok {
		t.Fatal("expected Update to route the delete and drop the workspace's tabs")
	}
}
